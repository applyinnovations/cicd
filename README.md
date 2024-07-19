CI/CD
---

## Running docker on M<X> Macs
Phase don't have build for arm64 yet, so we need to force docker to use amd64 for now by passing `--platform linux/amd64` to docker commands.
```
docker build --platoform linux/amd64 .
```
