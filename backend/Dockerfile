FROM golang:1.26-alpine AS build

WORKDIR /src

ENV GOPROXY=https://proxy.golang.org,direct
ENV GOSUMDB=sum.golang.org

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/dispatcher ./cmd/dispatcher
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/simulator ./cmd/simulator
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/loadgen ./cmd/loadgen

FROM alpine:3.20
WORKDIR /app
RUN adduser -D appuser
COPY --from=build /out/* /app/
USER appuser
