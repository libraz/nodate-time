# API 概要

Huma v2 が OpenAPI 記述を自動生成します。認証なしで `/openapi.json` と `/openapi.yaml` を配信し、`/docs` にリファレンスを描画します。ルートは `apps/api/internal/http/router/router.go` にあります。

以下のグループは網羅ではありません。全件はルーターを参照してください。

- **公開**: `/health`、`/openapi.json`、`/openapi.yaml`、`/docs`。`/auth/*`（`/auth/logout` を除く。register / login / refresh / password-reset / verify-email/confirm / oauth があり、`dev-login` は開発時のみ）。`/share/{token}` と `/share/{token}/events`。`/blobs/*` はセッションを持たず、URL の署名で認可されます。
- **要認証**: `/auth/logout`、`/user/*`、`/calendars`、`/calendars/{id}/{events,memos,members,invites,albums,labels,activity,export,import}`、`/invites/{token}/accept`。
- **管理者**: `/admin/oauth-providers`、`/admin/allowed-emails`、`/admin/instance-admins`。

サインイン・登録・パスワード再設定・OAuth（`/auth/*`）と公開共有リンク（`/share/*`）は IP 単位でレート制限されます。上限は[設定](configuration.md)の `TC_AUTH_RATE_LIMIT` と `TC_SHARE_RATE_LIMIT` で決まります。`/health`、OpenAPI と docs のルート、`/blobs/*` は対象外です。
