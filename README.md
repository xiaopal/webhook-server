# simple webhook server

```
docker run -it --rm -p 8080:8080 xiaopal/webhook-server env

curl 127.0.0.1:8080
```


# build/test
```
CGO_ENABLED=0 GOOS=linux go build -o bin/webhook-server -ldflags '-s -w' cmd/*.go

bin/webhook-server --location / -v --form-values --headers --json-handlers -- jq -n '{status: 200, headers: {}, body: (env|tojson)}'

bin/webhook-server --location / -v --data --json-handlers -- jq -sR '{status: 200, body: "echo: \(.)" }'

```
