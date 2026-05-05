#!/bin/bash
# docker-build.sh - Cross-platform Docker build wrapper
# Unsets DOCKER_DEFAULT_PLATFORM so each machine builds natively
# Usage: ./scripts/docker-build.sh [service] [compose-args...]
#
# ARM Mac  -> builds linux/arm64
# AMD64    -> builds linux/amd64
unset DOCKER_DEFAULT_PLATFORM
exec docker-compose build "$@"
