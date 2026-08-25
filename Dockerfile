# Dockerfile
FROM gcr.io/distroless/static
COPY dbtools /usr/local/bin/dbtools
WORKDIR /workspace
ENTRYPOINT ["dbtools"]
