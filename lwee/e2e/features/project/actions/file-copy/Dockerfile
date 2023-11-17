FROM golang:1.21 as builder

WORKDIR /app

# Copy module files for downloading dependencies.
COPY go.mod go.sum ./
RUN go mod download

# Copy remaining application files and build the application.
COPY . .
RUN CGO_ENABLED=0 go build -o /main main.go

FROM gcr.io/distroless/base

COPY --from=builder /main /main

CMD ["/main", "-src", "/lwee/src", "-dst", "/lwee/dst"]
