FROM ubuntu:24.04
RUN apt-get update -qq && DEBIAN_FRONTEND=noninteractive apt-get install -y -qq git zsh fish jq procps curl ca-certificates >/dev/null && rm -rf /var/lib/apt/lists/*
