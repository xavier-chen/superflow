ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}
RUN apt-get update && apt-get install -y --no-install-recommends python3 python3-cryptography pulseaudio pulseaudio-utils procps \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /work
ENV PYTHONPATH=/work/linux
ENV PYTHONDONTWRITEBYTECODE=1
RUN useradd --create-home soundflow
ENV HOME=/home/soundflow
USER soundflow
ENTRYPOINT []
CMD ["python3", "-m", "unittest", "discover", "-s", "tests/linux", "-v"]
