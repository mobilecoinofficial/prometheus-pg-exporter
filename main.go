package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	log = logrus.New()
)

type metrics struct {
	dbUp *prometheus.GaugeVec
}

type db struct {
	url         string
	redactedUrl string
	checkWait   time.Duration
	timeout     time.Duration
}

func main() {
	setupLogging()
	m := setupMetrics()
	d := setupDB()

	// start DB checks
	go checkDB(m, d)

	// serve metrics endpoint
	serve()
}

func checkDB(m *metrics, d *db) {
	for {
		// create a context with a timeout to stop hanging database timeouts.
		ctx := context.Background()
		ctx, cancel := context.WithTimeout(ctx, d.timeout)

		// test database connection
		db, err := sql.Open("pgx", d.url)
		if err != nil {
			log.Errorf("Unable to connect to database: %v", err)
			// Set metric to "unhealthy" and sleep until next round
			m.dbUp.WithLabelValues(d.redactedUrl).Set(0)
			cancel()
			time.Sleep(d.checkWait)
			continue
		}

		// Ping database and make sure connection is up
		err = db.PingContext(ctx)
		if err != nil {
			log.Errorf("Unable to PING database: %v", err)
			// Set metric to "unhealthy"
			m.dbUp.WithLabelValues(d.redactedUrl).Set(0)
			db.Close()
			cancel()
			// don't sleep if this is a deadline exceeded, we've waited enough
			if !strings.Contains(err.Error(), "context deadline exceeded") {
				time.Sleep(d.checkWait)
			}
			continue
		}

		// set metric to "healthy"
		log.Info("Database connection is healthy")
		m.dbUp.WithLabelValues(d.redactedUrl).Set(1)

		// Clean up
		db.Close()
		cancel()

		// sleep until next round
		time.Sleep(d.checkWait)
	}
}

func serve() {
	host := getEnv("LISTEN_HOST", "127.0.0.1")

	port, err := strconv.Atoi(getEnv("LISTEN_PORT", "9090"))
	if err != nil {
		log.Fatalf("Unable to parse LISTEN_PORT: %v", err)
	}

	log.Infof("Serving metrics at %s:%d/metrics", host, port)
	http.Handle("/metrics", promhttp.Handler())

	err = http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), nil)
	if err != nil {
		log.Fatalf("error serving http: %v", err)
	}
}

func setupDB() *db {
	dbUrl := os.Getenv("DATABASE_URL")
	u, err := url.Parse(dbUrl)
	if err != nil {
		// showing the value of err will leak the creds
		log.Fatalf("Unable to parse DATABASE_URL")
	}

	redactedUrl := fmt.Sprintf("%s://%s:xxxxx@%s%s", u.Scheme, u.User.Username(), u.Host, u.Path)
	log.Debugf("%s", redactedUrl)

	checkWait, err := strconv.Atoi(getEnv("CHECK_WAIT", "10"))
	if err != nil {
		log.Fatalf("Unable to parse CHECK_WAIT: %v", err)
	}

	timeout, err := strconv.Atoi(getEnv("TIMEOUT", "10"))
	if err != nil {
		log.Fatalf("Unable to parse TIMEOUT: %v", err)
	}

	return &db{
		url:         dbUrl,
		redactedUrl: redactedUrl,
		checkWait:   time.Duration(checkWait) * time.Second,
		timeout:     time.Duration(timeout) * time.Second,
	}
}

func setupMetrics() *metrics {
	m := &metrics{}

	// this sets the metric name mc_database_ping with a label of the redacted url
	m.dbUp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "mc",
		Subsystem: "database",
		Name:      "ping",
		Help:      "PING works!",
	}, []string{"url"})

	return m
}

func setupLogging() {
	logLevel, err := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		log.Fatalf("Invalid LOG_LEVEL: %v\n", err)
	}
	log.SetLevel(logLevel)
	log.SetFormatter(&logrus.JSONFormatter{})
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
