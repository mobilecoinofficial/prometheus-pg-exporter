package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var log = logrus.New()

type metrics struct {
	dbUp *prometheus.GaugeVec
}

func main() {
	logLevel, err := logrus.ParseLevel(getEnv("LOG_LEVEL", "info"))
	if err != nil {
		fmt.Printf("Invalid LOG_LEVEL: %v\n", err)
		os.Exit(1)
	}
	log.SetLevel(logLevel)
	log.SetFormatter(&logrus.JSONFormatter{})

	m := setupMetrics()

	go checkDB(m)

	listenHost := getEnv("LISTEN_HOST", "127.0.0.1")
	listenPort, err := strconv.Atoi(getEnv("LISTEN_PORT", "9090"))
	if err != nil {
		log.Errorf("Unable to parse LISTEN_PORT: %v", err)
		os.Exit(1)
	}
	serve(listenHost, listenPort)
}

func checkDB(m *metrics) {
	for {
		// grab the url parse and redact user:pass
		dbUrl := os.Getenv("DATABASE_URL")
		u, err := url.Parse(dbUrl)
		if err != nil {
			// showing the value of err will leak the creds
			log.Errorf("Unable to parse DATABASE_URL")
			os.Exit(1)
		}

		redactedUrl := fmt.Sprintf("%s://%s:xxxxx@%s%s", u.Scheme, u.User.Username(), u.Host, u.Path)
		log.Debugf("%s", redactedUrl)

		checkWait, err := strconv.Atoi(getEnv("CHECK_WAIT", "10"))
		if err != nil {
			log.Errorf("Unable to parse CHECK_WAIT: %v", err)
			os.Exit(1)
		}
		log.Debugf("Wait %d seconds before checking DB", checkWait)
		time.Sleep(time.Duration(checkWait) * time.Second)

		// test database connection
		db, err := sql.Open("pgx", dbUrl)
		if err != nil {
			log.Errorf("Unable to connect to database: %v", err)
			m.dbUp.WithLabelValues(redactedUrl).Set(0)
			continue
		}

		// Ping database and make sure connection is up
		err = db.Ping()
		if err != nil {
			log.Errorf("Unable to PING database: %v", err)
			m.dbUp.WithLabelValues(redactedUrl).Set(0)
			db.Close()
			continue
		}

		// If we can query our db set value as up
		log.Info("Database connection is healthy")
		m.dbUp.WithLabelValues(redactedUrl).Set(1)
		db.Close()
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

func serve(host string, port int) {
	log.Infof("serving metrics at %s:%d/metrics", host, port)
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(fmt.Sprintf("%s:%d", host, port), nil)
	if err != nil {
		log.Errorf("error serving http: %v", err)
		return
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
