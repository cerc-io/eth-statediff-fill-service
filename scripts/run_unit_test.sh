#!/bin/bash

set -ex

# # Clear up existing docker images and volume.
# docker-compose down --remove-orphans --volumes

# # Spin up TimescaleDB
# docker-compose -f docker-compose.yml up -d migrations ipld-eth-db
# # sleep 45
# sleep 10

# Run unit tests
# go clean -testcache
PGPASSWORD=password DATABASE_USER=vdbm DATABASE_PORT=8099 DATABASE_PASSWORD=password DATABASE_HOSTNAME=localhost DATABASE_NAME=vulcanize_testing exec "$@"

# # Clean up
# docker-compose down --remove-orphans --volumes
