# Stage 1: Build the Go program
FROM golang:1.24-alpine AS build
WORKDIR /opt/msp-go

# Copy the project files and build the program
COPY . .
RUN apk --no-cache add gcc musl-dev
RUN cd root && \
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o mainroot main.go

# Stage 2: Copy the built Go program into a minimal container
FROM alpine:3.21
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /opt/msp-go/root/mainroot /app/

RUN chmod +x /app/mainroot

CMD ["/app/mainroot"]

# Build Image with command
# docker buildx create --use
# docker buildx build \
#   --platform linux/amd64,linux/arm64 \
#   -t mochigome/msp-go:2.2v.ecs \
#   --push .

# legacy build
# docker build -t msp-go:2.22v.ecs .
# docker tag  msp-go:2.22v.ecs mochigome/msp-go:2.22v.ecs
# docker push mochigome/msp-go:2.22v.ecs

# AWS ECR
# docker tag msp-go:2.22v.ecs 590183751536.dkr.ecr.ap-southeast-1.amazonaws.com/msp-go:2.22v.ecs
# docker push 590183751536.dkr.ecr.ap-southeast-1.amazonaws.com/msp-go:2.22v.ecs

# current version : 2.22v.ecs