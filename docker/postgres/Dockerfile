# syntax=docker/dockerfile:1
# Custom PostgreSQL image for capysquash validation.
# Includes PostGIS and the extensions the migration tests exercise.
#
# The healthcheck lives in the compose files, not here, so it can be tuned per
# environment and per database name.

ARG PG_VERSION=18
FROM postgres:${PG_VERSION}

# Re-declared so it is in scope for the LABEL below - args from before the
# first FROM are not automatically visible inside a stage.
ARG PG_VERSION

RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql-${PG_MAJOR}-postgis-3 \
    postgresql-${PG_MAJOR}-postgis-3-scripts \
    postgresql-contrib-${PG_MAJOR} \
    postgresql-${PG_MAJOR}-pgtap \
  && rm -rf /var/lib/apt/lists/*

ENV POSTGRES_INITDB_ARGS="--auth-host=scram-sha-256"

LABEL org.capysquash.image.type="postgres-validation" \
      org.capysquash.postgres.version="${PG_VERSION}" \
      org.capysquash.extensions="postgis,cube,earthdistance,pg_trgm,pg_stat_statements,btree_gin,btree_gist,pgcrypto,uuid-ossp"
