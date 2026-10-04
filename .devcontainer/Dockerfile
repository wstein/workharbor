# The non-root user has a passwd entry because
# ssh-keygen, which the tests run, refuses a uid that has none.
# Pinned by digest; Dependabot (docker) keeps the tag and digest current.
FROM golang:1.27.1-trixie@sha256:3b77fc618ec235a1ab412de7737f120dd507c57e8d87de4cbb7994fb94275ed5

ENV GOTOOLCHAIN=local

RUN useradd --create-home --uid 1000 --shell /bin/bash agent
USER agent
