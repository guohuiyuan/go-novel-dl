# Build stage
FROM --platform=$BUILDPLATFORM golang:1.25 AS builder

WORKDIR /app
ARG TARGETOS=linux
ARG TARGETARCH

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=${TARGETARCH:-$(go env GOARCH)} go build -trimpath -ldflags "-s -w" -o novel-dl ./cmd/novel-dl

# Runtime stage
FROM alpine:3.22

RUN apk --no-cache add ca-certificates tzdata

ENV TZ=Asia/Shanghai

RUN adduser -D -s /bin/sh appuser

WORKDIR /home/appuser/

COPY --from=builder /app/novel-dl .
RUN chown -R appuser:appuser /home/appuser/

USER appuser

EXPOSE 8080

CMD ["./novel-dl", "web", "--port", "8080", "--no-browser"]
