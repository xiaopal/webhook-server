# simple webhook server

```
# build/test
```
CGO_ENABLED=0 GOOS=linux go build -o bin/webhook-server -ldflags '-s -w' cmd/*.go

bin/webhook-server webhook-server --location / -v --form-values --headers --json-handlers -- jq -n '{status: 200, headers: {}, body: (env|tojson)}'

```
