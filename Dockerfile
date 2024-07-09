# syntax=docker/dockerfile:1

FROM golang:1.22

# Install Docker CLI
RUN apt-get update && \
    apt-get install -y apt-transport-https ca-certificates curl gnupg lsb-release jq && \
    curl -fsSL https://download.docker.com/linux/static/stable/x86_64/ \
    | grep -Eo 'docker-[0-9]+\.[0-9]+\.[0-9]+\.tgz' \
    | sort -V \
    | tail -n 1 \
    | xargs -I {} curl -fsSL -o docker.tgz https://download.docker.com/linux/static/stable/x86_64/{} && \
    tar xzvf docker.tgz --strip 1 -C /usr/local/bin docker/docker && \
    rm docker.tgz

WORKDIR /usr/src/app

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY *.go ./

RUN go build -v -o /usr/local/bin/app ./...

CMD ["app"]
