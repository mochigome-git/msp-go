# Stage 1: Build the Go program
FROM golang:1.21-alpine AS build
WORKDIR /opt/nk3-PLCcapture-go

# Copy the project files and build the program
COPY . .
RUN apk --no-cache add gcc musl-dev
RUN cd 2.0v && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main2.0v main.go

# Stage 2: Copy the built Go program into a minimal container
FROM alpine:3.14
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /opt/nk3-PLCcapture-go/2.0v/main2.0v /app/
COPY 2.0v/.env.local /app/.env.local

RUN chmod +x /app/main2.0v

CMD ["/app/main2.0v"]

# Build Image with command
# docker build -t nk3-msp:${version} .
