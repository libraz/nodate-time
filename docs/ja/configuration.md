# 設定

環境変数はすべて `TC_` プレフィックスです。表には主要なものを載せています。全件と既定値は `apps/api/internal/config/config.go` にあります。`compose.prod.yml` がどれをそのまま渡し、どれを固定するかは[デプロイ](deployment.md)を参照してください。

| 変数 | 用途 | デフォルト |
| --- | --- | --- |
| `TC_ENV` | `development` で開発用機能を有効化 | `production` |
| `TC_APP_NAME` | デプロイの名乗り（OpenAPI のタイトル、送信メール、iCalendar の `PRODID`）。SPA へはビルド引数 `VITE_APP_NAME` で同じ名前を渡す | `Nodate Time` |
| `TC_PORT` | API ポート | `8080` |
| `TC_DB_DSN` | MySQL DSN | `ttuser:ttpw@tcp(127.0.0.1:33306)/timetree_clone?parseTime=true` |
| `TC_JWT_SECRET` | JWT 署名鍵（32 バイト以上）。全環境で必須。組み込みの値はこのリポジトリで公開されているため拒否される | なし（`make dev` が `secrets/` 配下に生成） |
| `TC_PASSWORD_LOGIN_ENABLED` | メール+パスワード認証の有効化 | `true` |
| `TC_WORKSPACE_TIMEZONE` | 新規アカウントの初期タイムゾーン（IANA 名）。サインアップもプロバイダ初回サインインもゾーンを聞かず、列は NOT NULL。tz データベースにない名前は起動時に弾く | `Asia/Tokyo` |
| `TC_DEFAULT_LOCALE` | 新規アカウントの初期ロケール（BCP 47 タグ）。理由は上と同じ | `ja` |
| `TC_CORS_ALLOWED_ORIGINS` | CORS 許可オリジン（カンマ区切り）。空と `*` は全環境で拒否される | localhost:5173 |
| `TC_TRUSTED_PROXIES` | `X-Forwarded-For` の設定を信頼するリバースプロキシ（CIDR または IP、カンマ区切り）。プロキシ配下で未設定だと全クライアントが 1 つのレート制限バケットに集約される | 信頼しない |
| `TC_AUTH_RATE_LIMIT` | サインイン・登録・パスワード再設定・OAuth の IP あたり req/min | `60` |
| `TC_SHARE_RATE_LIMIT` | 公開共有リンクの IP あたり req/min（サインインとは別枠） | `600` |
| `TC_WEB_URL` / `TC_API_PUBLIC_URL` | フロント / API の公開 URL。`TC_API_PUBLIC_URL` は blob URL が指し戻す先でもあるため、API を置いたパスまで含める必要がある | localhost |
| `TC_S3_ENDPOINT` / `TC_S3_ACCESS_KEY` / `TC_S3_SECRET_KEY` / `TC_S3_BUCKET` / `TC_S3_USE_SSL` | S3 互換ストレージ。到達するのは API だけで、ブラウザからは触らせない。`minioadmin` の認証情報は `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` がない限り拒否される | ローカルストレージの既定値 |
| `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` | 広く知られた `minioadmin` 認証情報を許可する（ローカルのオブジェクトストア用） | `false`（`make dev` は `true`） |
| `TC_SMTP_HOST` ほか `TC_SMTP_*` | メール送信。空ならメール送信はオフで、これは全環境で有効な設定。API は起動し、送信オフをログに出し、パスワード再設定とメール確認を提供しない（auth-providers のレスポンスは `mailEnabled: false` を返し、SPA は該当の画面を隠す） | 未設定（`make dev` は mailpit を指す） |
| `TC_GOOGLE_CLIENT_ID` / `TC_GOOGLE_CLIENT_SECRET` | Google OAuth | 空 |
| `TC_GOOGLE_ALLOWED_DOMAINS` | サインアップ / サインインを許可するメールドメイン（カンマ区切り）。パスワードの登録・ログインと Google サインインが検査する。LINE はメールアドレスを検証しないため、リストを設定している間は拒否される。リスト外のアドレスでも、個別にアロウリストへ登録されていれば通り、`createuser` は作成したアカウントをアロウリストに登録する | 制限なし |
| `TC_LINE_CLIENT_ID` / `TC_LINE_CLIENT_SECRET` | LINE OAuth | 空 |
| `TC_SECRETS_KEY` | DB に保存する秘密情報の暗号鍵（hex/base64 32 バイト） | 空 |
| `TC_METRICS_ADDR` | 管理用リスナーの待ち受けアドレス。空で無効化 | `127.0.0.1:9464` |
| `TC_METRICS_ALLOWED_CIDRS` | 管理用リスナーに到達できるネットワーク。直接の TCP ピアと照合する。空はすべて拒否 | loopback + プライベート帯 |

## 起動時の検証

`Config.Validate()` は `TC_ENV=development` を含む全環境で働きます。次の設定では起動を拒否します。

- 組み込みの JWT 秘密鍵、または 32 バイト未満の JWT 秘密鍵
- 既定の `minioadmin` 認証情報
- 空またはワイルドカードの CORS
- tz データベースにない `TC_WORKSPACE_TIMEZONE`

許可用の変数があるのは、ストレージの認証情報の `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` だけです。他のガードに許可変数はありません。`TC_ENV=development` が足すのは `/auth/dev-login` だけです。

## ローカルスタックの変数

`compose.yml` は `TC_DB_PORT`（既定 33306）、`TC_DB_ROOT_PASSWORD`、`TC_DB_NAME`、`TC_DB_USER`、`TC_DB_PASSWORD`、`TC_S3_PORT`（既定 9000、ループバックのみ）、`TC_MAILPIT_SMTP_PORT`（既定 1025）、`TC_MAILPIT_UI_PORT`（既定 8025）を読みます。mailpit の 2 つのポートはどちらもループバックのみです。
