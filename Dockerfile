FROM golang:1.10 as build
ADD . /go/src/github.com/xiaopal/webhook-server
WORKDIR  /go/src/github.com/xiaopal/webhook-server
RUN CGO_ENABLED=0 GOOS=linux go build -o /webhook-server -ldflags '-s -w' cmd/*.go
RUN chmod +x /webhook-server

FROM alpine:3.7

RUN apk add --no-cache bash coreutils curl && \
	curl -sSL "http://npc.nos-eastchina1.126.net/dl/dumb-init_1.2.0_amd64.tar.gz" | tar -zx -C /usr/bin && \
	curl -sSL 'http://npc.nos-eastchina1.126.net/dl/jq_1.5_linux_amd64.tar.gz' | tar -zx -C /usr/bin

COPY --from=build /webhook-server /webhook-server
RUN ln -s /webhook-server /usr/bin/webhook-server

ENTRYPOINT [ "/usr/bin/dumb-init", "/webhook-server" ]
