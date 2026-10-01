FROM fedora:latest
RUN dnf -q -y install git zsh fish jq procps-ng curl && dnf clean all
