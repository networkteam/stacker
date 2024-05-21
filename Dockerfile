FROM alpine:3.19
ENTRYPOINT ["/stacker"]
STOPSIGNAL SIGINT
COPY stacker /stacker
