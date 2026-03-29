FROM golang:1.25-alpine AS builder

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./

RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o novel-dl ./cmd/novel-dl

FROM alpine:latest

RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk --no-cache add ca-certificates tzdata

ENV TZ=Asia/Shanghai

RUN adduser -D -s /bin/sh appuser

WORKDIR /home/appuser

COPY --from=builder /app/novel-dl ./

RUN chown -R appuser:appuser /home/appuser

USER appuser

EXPOSE 8080

CMD ["./novel-dl", "web", "--port", "8080", "--no-browser"]
