FROM golang:1.25-alpine AS builder
WORKDIR /app
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOPROXY=https://goproxy.cn,direct
COPY . .
RUN cd ./backend/cmd/server \
    && go build -o deepwiki main.go


FROM alpine:3.9
RUN apk add --no-cache ca-certificates tzdata \
&& cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime \
&& echo "Asia/Shanghai" > /etc/timezone
WORKDIR /app

COPY --from=builder /app/backend/cmd/server/deepwiki ./

EXPOSE 8080

CMD ["./deepwiki"]
