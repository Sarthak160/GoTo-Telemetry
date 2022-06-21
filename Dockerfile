FROM golang:alpine as builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./

RUN go mod download

COPY *.go ./

RUN go build -o /docker-gs-ping

EXPOSE 8085

CMD [ "/docker-gs-ping" ]