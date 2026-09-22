# Docker

Everything here exists for one job: giving `capysquash` a PostgreSQL to validate
against without installing one.

| File | What it is |
| --- | --- |
| `../Dockerfile` | The `capysquash` CLI image (CGO build, `ENTRYPOINT ["capysquash"]`) |
| `../compose.yaml` | `capysquash` + `postgres-primary` (PostgreSQL 18 with PostGIS and contrib) |
| `../compose.testing.yaml` | Overlay adding PostgreSQL 15, 16 and 17 |
| `../compose.tools.yaml` | Overlay adding pgAdmin and Filebrowser behind the `tools` profile |
| `postgres/Dockerfile` | The validation PostgreSQL image (`PG_VERSION` build arg: 15–18) |
| `init-scripts/` | Init SQL mounted into every validation database: extensions and a Supabase compatibility layer (`auth.*`, `storage.*`) |
| `validation/with-validation.yml` | The CLI as root, for Docker-based `validate` (see below) |

## Usage

```bash
# the CLI, no services
docker build -t capysquash .
docker run --rm -v "$PWD/migrations:/app/migrations:ro" capysquash analyze /app/migrations

# CLI + a validation database
docker compose up -d postgres-primary
docker compose run --rm capysquash squash /app/migrations --output /app/output

# several PostgreSQL majors
docker compose -f compose.yaml -f compose.testing.yaml up -d

# pgAdmin (http://127.0.0.1:5050) and Filebrowser (http://127.0.0.1:8081)
docker compose -f compose.yaml -f compose.tools.yaml --profile tools up -d
```

The `capysquash` service is a one-shot CLI: drive it with `docker compose run`,
never `up`. Configure it with `.env` (copy `.env.example`); every variable the
compose files read is listed there.

## Docker-based validation from inside the container

`capysquash validate` and post-squash validation spin up throwaway PostgreSQL
containers on the host daemon, which needs `/var/run/docker.sock`. That socket
is `root:root 0660`, so the image's non-root uid cannot open it; the
`validation/with-validation.yml` stack runs the CLI as root for exactly this
case. Mounting the socket hands the container control of the host daemon:
development use only.

```bash
docker compose -f docker/validation/with-validation.yml run --rm \
  capysquash validate /app/migrations /app/output
```

Prefer running the CLI natively when you can; it only needs Docker on the host.
