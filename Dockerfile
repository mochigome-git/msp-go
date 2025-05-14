# Stage 1: Build the Go program
FROM golang:1.24-alpine AS build
WORKDIR /opt/nk3-PLCcapture-go

# Copy the project files and build the program
COPY . .
RUN apk --no-cache add gcc musl-dev
RUN cd root && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o mainroot main.go

# Stage 2: Copy the built Go program into a minimal container
FROM alpine:3.21
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /opt/nk3-PLCcapture-go/root/mainroot /app/

RUN chmod +x /app/mainroot

CMD ["/app/mainroot"]

# Build Image with command
# docker build -t msp-go:${version} .
# docker tag  msp-go:${version} mochigome/msp-go:${version}
# docker push mochigome/ msp-go:tagname

# current version : 2.15v.ecs