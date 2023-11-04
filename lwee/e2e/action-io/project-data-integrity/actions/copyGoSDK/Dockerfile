FROM golang:1.21 as builder

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build main.go

FROM ubuntu:20.04

COPY --from=builder /app/main /app/main

ENTRYPOINT ["/app/main"]
