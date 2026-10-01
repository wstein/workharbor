# The Go toolchain matches go.mod. The non-root user has a passwd entry because
# ssh-keygen, which the tests run, refuses a uid that has none.
FROM golang:1.27.1-trixie

RUN useradd --create-home --uid 1000 --shell /bin/bash agent
USER agent
