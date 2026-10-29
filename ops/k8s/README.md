# Kubernetes manifests (starting point, not yet applied to a cluster)

Datastores (PostgreSQL+PostGIS, Redis, MongoDB) are expected to be managed services; only the three stateless services are deployed here.

1. Build and push images, then replace `connectme-*:latest` in `workloads.yaml`.
2. Create the secret from the `.env.example` keys (never commit it):
   `kubectl -n connectme create secret generic connectme-secrets --from-env-file=.env.production`
   It must include `POSTGRES_URL`, `REDIS_URL`, `MONGO_URL`, `JWT_SECRET`, `AI_SERVICE_KEY`, the Stripe keys, `APP_ALLOWED_ORIGINS`, and `TRUSTED_PROXY_CIDRS` (your ingress CIDR, so rate limits see real client IPs).
   The API refuses to start in production without: `AI_SERVICE_URL` and `AI_SERVICE_KEY` (content moderation fails closed without the AI service), a real `CSAM_PROVIDER` (see `docs/CSAM_RUNBOOK.md`; none has an adapter yet, so a production start is blocked until one is written), and `MODERATION_DEV_BYPASS` unset. `ENV=production` is set in `workloads.yaml`; any ENV other than `development` or `test`, or none, counts as production. Tune `AI_TEXT_TIMEOUT_MS` / `AI_IMAGE_TIMEOUT_MS` if needed.
   Push needs `PUSH_PROVIDER=fcm`, `FCM_PROJECT_ID` and the service-account key mounted as a secret at the path in `FIREBASE_CREDENTIALS`; `api-egress` must allow TCP 443 to `oauth2.googleapis.com` and `fcm.googleapis.com`. Short videos and reels are mounted only with `ENABLE_VIDEO=true` and need `VIDEO_SCANNER=http` with `VIDEO_SCANNER_URL`/`VIDEO_SCANNER_KEY` in production (`docs/AUDIT.md` 3.10). For live streaming, audio rooms and calls also add `LIVEKIT_URL`, `LIVEKIT_API_KEY` and `LIVEKIT_API_SECRET` (the `/live` routes are mounted only when `LIVEKIT_URL` is set); the LiveKit server itself is not deployed by these manifests, and `api-egress` allows only TCP 443 to it, so add its port if you self-host.
3. `kubectl apply -f workloads.yaml -f network-policies.yaml`
4. Ingress, TLS and autoscaling are cluster-specific and not included. Never route `/metrics` or port 9464 through the ingress; the NetworkPolicy lets only the `monitoring` namespace scrape it.

With `S3_ENDPOINT` set in the secret the API stores media in the bucket and the `connectme-media` PVC and `/media` mount in `workloads.yaml` can be removed.

`connectme-api` binds metrics to `0.0.0.0:9464` here because the pod network is private; locally it defaults to `127.0.0.1`.

Validate without a cluster: `kubectl apply --dry-run=client -f ops/k8s/`.
