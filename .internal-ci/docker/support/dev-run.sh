#!/bin/bash

dlv_pid=""

function cleanup ()
{
  echo "kill dlv"
  kill "${dlv_pid}"
}

trap "cleanup" EXIT INT

echo "-- Development environment start up --"
echo "--- Install dev tools ---"
go install github.com/go-delve/delve/cmd/dlv@latest

echo "--- Install dependencies ---"
go mod vendor

echo "--- Build code ---"
go build -v -o app

# start debugger and open port for remote
echo "--- Run with dlv debugger - port 2345 ---"
dlv exec ./app --headless --listen=:2345 --continue --api-version=2 --accept-multiclient &

dlv_pid=$!

# Improvements: Just monitor .go files?
echo "--- inotifywait - monitor and bump container on changes ---"
inotifywait -e create -e modify -e delete -e move --exclude '(\.git/|vendor/|^app$)' -r /app
