# syntax=docker/dockerfile:1.7
# Multi-stage build for the nodate-time React SPA.
# The build context is the repository root: the lockfile and the shared
# TypeScript configuration live there.

FROM oven/bun:1.3-alpine AS build
WORKDIR /src

COPY package.json bun.lock bunfig.toml tsconfig.base.json tsconfig.json ./
COPY apps/web/package.json ./apps/web/
# The root package's preinstall script runs `node` to refuse npm and yarn.
# This image has bun and no node, and the check has already been answered by
# the only runtime present.
RUN bun install --frozen-lockfile --ignore-scripts

COPY apps/web ./apps/web

# The API origin is compiled into the bundle -- the SPA reads it from
# import.meta.env, which Vite substitutes at build time -- so an image built
# for one deployment cannot be pointed at another. Moving it means rebuilding.
ARG VITE_API_BASE
ENV VITE_API_BASE=${VITE_API_BASE}

# What the deployment calls itself, in the document title and wherever the SPA
# names itself. Compiled in for the same reason as the origin above, and left
# unset by a build that is content with the software's own name.
ARG VITE_APP_NAME
ENV VITE_APP_NAME=${VITE_APP_NAME}

RUN cd apps/web && bun run build

FROM nginx:1.29-alpine
# app.conf holds what is served either way and is included by both server
# blocks. The default is plain HTTP, for a deployment where something else
# terminates TLS; compose.prod.yml mounts the TLS template over it.
COPY apps/web/nginx/app.conf /etc/nginx/app.conf
COPY apps/web/nginx/http.conf /etc/nginx/conf.d/default.conf
COPY --from=build /src/apps/web/dist /usr/share/nginx/html
EXPOSE 80 443
