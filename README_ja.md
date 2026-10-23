# Nodate Time

[![CI](https://github.com/libraz/nodate-time/actions/workflows/ci.yml/badge.svg)](https://github.com/libraz/nodate-time/actions/workflows/ci.yml)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](./LICENSE)
![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)
![MySQL](https://img.shields.io/badge/MySQL-8.4-4479A1?logo=mysql&logoColor=white)
![Bun](https://img.shields.io/badge/Bun-1.4-000000?logo=bun&logoColor=white)

共有カレンダーアプリ。家族・チーム・タレント事務所など、複数人で 1 つのカレンダーを運用するためのグループカレンダー。Go 製の REST API と React SPA のモノレポ構成。

## スクリーンショット

| 月表示 | 週表示 |
| --- | --- |
| ![イベント作成ダイアログを開いた月表示](./docs/images/monthly-create-event.png) | ![週表示のタイムライン](./docs/images/weekly-view.png) |
| **共有・埋め込み** | |
| ![招待リンクと埋め込みコードの共有パネル](./docs/images/share-panel.png) | |

モバイル表示（縦スクロールの月表示）:

<p align="left">
  <img src="./docs/images/mobile-monthly.png" width="280" alt="モバイルの月表示">
</p>

## 主な機能

- **カレンダー共有** — メンバーごとのロール（owner / manager / editor / viewer）、招待リンク発行、公開共有トークン、外部埋め込み（embed）ビュー
- **予定（イベント）** — 終日・時間指定、繰り返し（RRULE）と例外日編集、ドラッグでの移動・リサイズ、参加者・チェックリスト・添付ファイル・コメント
- **メモ** — カレンダー単位のメモ
- **アルバム** — カレンダーごとの写真アルバム（イベントへの紐付け可）
- **アクティビティ / 監査履歴** — イベント・メモの変更履歴とカレンダー単位のアクティビティフィード
- **認証** — メール+パスワード、Google / LINE の OAuth/OIDC ログイン、パスワードリセット、開発用パスワードレスログイン
- **管理者機能** — OAuth プロバイダ設定、サインイン許可メール/ドメインのアロウリスト
- **エクスポート / インポート** — エクスポートは iCal・CSV、インポートは iCal
- **多言語 / テーマ** — 日本語・英語の i18n、テーマ切り替え

## 技術スタック

| 領域 | 採用技術 |
| --- | --- |
| API | Go 1.26 + [Huma v2](https://huma.rocks)（OpenAPI）+ chi/v5 + [sqlc](https://sqlc.dev) |
| DB | MySQL 8.4（Docker Compose） |
| オブジェクトストレージ | S3 互換（ローカルは MinIO） |
| Web | React 19 + TypeScript + Vite + TanStack Router + Zustand + Tailwind CSS 4 + Luxon |
| Lint / Format | Biome（Web）/ gofmt（Go） |
| パッケージマネージャ | Bun |
| テスト | Go E2E（testify + 実 MySQL）/ Vitest（Web） |

## ディレクトリ構成

```
apps/
  api/                 Go REST API
    cmd/api/           エントリポイント
    cmd/createuser/    ユーザー作成 CLI
    cmd/healthcheck/   コンテナのヘルスチェック
    internal/          config, auth, cleanup, db/generated, errors,
                       eventlog, mailer, recurrence, secrets, storage,
                       http/{handlers,middleware,router}
    tests/e2e/         E2E テスト
  web/                 React SPA
    src/components/     UI コンポーネント
    src/routes/         TanStack Router（file-based, share/ embed/ 含む）
    src/stores/         Zustand（auth / calendar / ui ストア）
    src/lib/            ユーティリティ（date, recurrence, upload, theme 等）
    src/i18n/           日本語 / 英語ロケール
sql/
  core/                共有スキーマ契約の vendor コピー（上流に固定、編集不可）
    tables/             共有テーブル定義
    triggers/           共有トリガ
    conformance/        契約適合チェック
  time/                本アプリ固有のテーブル
    tables/             テーブル定義
    alters/             このアプリが core のテーブルに加える変更
    views/              ビュー定義
  upgrades/            既存の DB を今のスキーマへ上げる SQL
  queries/             sqlc クエリ定義
  schema.sql           build-schema.sh で生成される結合スキーマ
  sqlc.yaml            sqlc 設定
compose.yml            MySQL + MinIO のローカルスタック
compose.prod.yml       自己ホスト用スタック（MySQL + MinIO + API + Web）
```

## セットアップ

### 必要なもの

- [Bun](https://bun.sh) 1.4 以上（それより古い版は lockfile の形式を読めない）
- Go 1.26+
- Docker（MySQL / MinIO 用）
- [sqlc](https://sqlc.dev)（コード生成する場合）

### 起動

```bash
bun install            # 依存インストール（Bun 必須）
make db-up             # MySQL 起動（compose.yml）
make minio-up          # MinIO 起動（添付・アルバム・アバターに必要）
make db-apply          # スキーマ生成・適用
make db-seed-users     # demo@example.com / admin@example.com を作成
make dev               # DB + API + Web を一括起動
```

- API: <http://localhost:8080>（`/health`, OpenAPI は Huma が生成）
- Web: <http://localhost:5173>

`make dev` が起動するのは MySQL だけなので、`make minio-up` をしないと添付・アルバム・アバターのエンドポイントは 503 を返す。

開発環境（`TC_ENV=development`）では `/auth/dev-login` でシードユーザーにパスワードレスログインできる。

### よく使うコマンド

```bash
make dev               # DB + API + Web を並列起動
make db-up / db-down   # MySQL の起動 / 停止
make db-apply          # スキーマ適用（build-schema.sh → schema.sql）
make db-seed           # スキーマ適用 + ユーザー + サンプルデータ
make minio-up          # MinIO（S3 互換ストレージ）起動
make sqlc              # sqlc コード生成
make api / make web    # 個別起動
make build-api         # API バイナリビルド（bin/api）
make create-user ARGS="-email a@b.com -password secret123 -admin"
make format            # gofmt + Biome 自動修正
make lint              # gofmt チェック + Biome（書き込みなし）
make test-api          # Go ユニットテスト
make test-e2e          # E2E（要 MySQL + TC_TEST_INTEGRATION=1）
make test-e2e-storage  # MinIO を使うストレージ系も含めた E2E

bun run check          # Biome チェック
bun run typecheck      # TypeScript 型チェック（tsc -b）
cd apps/web && bun run test   # Web ユニットテスト（Vitest）
```

## 環境変数

すべて `TC_` プレフィックス。主要なもの（詳細・デフォルト値は `apps/api/internal/config/config.go`）:

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
| `TC_S3_ENDPOINT` / `TC_S3_ACCESS_KEY` / `TC_S3_SECRET_KEY` / `TC_S3_BUCKET` / `TC_S3_USE_SSL` | S3 互換ストレージ。到達するのは API だけで、ブラウザからは触らせない。`minioadmin` の認証情報は `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` がない限り拒否される | MinIO デフォルト |
| `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS` | 公開済みの MinIO 認証情報を許可する（ローカルのオブジェクトストア用） | `false`（`make dev` は `true`） |
| `TC_SMTP_HOST` ほか `TC_SMTP_*` | メール送信。ホスト未設定だと `TC_ALLOW_CONSOLE_MAILER` がない限り API は起動を拒否する | 未設定 |
| `TC_ALLOW_CONSOLE_MAILER` | SMTP リレーなしで動かす。メールは配送されずログに書かれる。開発環境以外ではログに残るのは件名だけで、宛先も本文も出ない | `false`（`make dev` は `true`） |
| `TC_GOOGLE_CLIENT_ID` / `TC_GOOGLE_CLIENT_SECRET` | Google OAuth | 空 |
| `TC_GOOGLE_ALLOWED_DOMAINS` | サインアップ / サインインを許可するメールドメイン（カンマ区切り）。パスワードの登録・ログインと Google サインインが検査する。LINE はメールアドレスを検証しないため、リストを設定している間は拒否される。リスト外のアドレスでも、個別にアロウリストへ登録されていれば通り、`createuser` は作成したアカウントをアロウリストに登録する | 制限なし |
| `TC_LINE_CLIENT_ID` / `TC_LINE_CLIENT_SECRET` | LINE OAuth | 空 |
| `TC_SECRETS_KEY` | DB に保存する秘密情報の暗号鍵（hex/base64 32 バイト） | 空 |
| `TC_METRICS_ADDR` | 管理用リスナーの待ち受けアドレス。空で無効化 | `127.0.0.1:9464` |
| `TC_METRICS_ALLOWED_CIDRS` | 管理用リスナーに到達できるネットワーク。直接の TCP ピアと照合する。空はすべて拒否 | loopback + プライベート帯 |

> `Config.Validate()` は `TC_ENV=development` を含む全環境で働く。組み込みまたは 32 バイト未満の JWT 秘密鍵・デフォルトの MinIO 認証情報・空またはワイルドカードの CORS・未設定の SMTP ホストを起動時に拒否し、tz データベースにない `TC_WORKSPACE_TIMEZONE` も弾く。許可用の変数があるのは 2 つだけで、MinIO 認証情報には `TC_ALLOW_DEFAULT_OBJECT_STORAGE_CREDENTIALS`、SMTP ホストには `TC_ALLOW_CONSOLE_MAILER`。他のガードに許可変数はない。`TC_ENV=development` が足すのは `/auth/dev-login` と、コンソールメーラーがメール全文（宛先も本文も）をログに書くことだけ。

DB / ストレージ系（`compose.yml`）: `TC_DB_PORT`（既定 33306）, `TC_DB_ROOT_PASSWORD`, `TC_DB_NAME`, `TC_DB_USER`, `TC_DB_PASSWORD`, `TC_MINIO_API_PORT`, `TC_MINIO_CONSOLE_PORT`。

## API 概要

Huma v2 で OpenAPI 記述が自動生成される。認証なしで `/openapi.json` と `/openapi.yaml` を配信し、`/docs` にリファレンスを描画する。ルートは `apps/api/internal/http/router/router.go` 参照。

以下のグループは網羅ではない。全件はルーターを参照。

- **公開** — `/health`, `/openapi.json`, `/openapi.yaml`, `/docs`、`/auth/*`（`/auth/logout` を除く。register/login/refresh/password-reset/verify-email/confirm/oauth、`dev-login` は開発時のみ）、`/share/{token}` と `/share/{token}/events`、`/blobs/*`（セッションは持たず、URL の署名で認可される）
- **要認証** — `/auth/logout`, `/user/*`, `/calendars`, `/calendars/{id}/{events,memos,members,invites,albums,labels,activity,export,import}`, `/invites/{token}/accept`
- **管理者** — `/admin/oauth-providers`, `/admin/allowed-emails`, `/admin/instance-admins`

サインイン・登録・パスワード再設定・OAuth（`/auth/*`）と公開共有リンク（`/share/*`）は IP 単位でレート制限される。`/health`、OpenAPI と docs のルート、`/blobs/*` は対象外。

## 可観測性

サーバーは 2 つのポートを持つ。`TC_PORT`（8080）がプロダクト、`TC_METRICS_ADDR`（127.0.0.1:9464）はプロダクトではなくプロセス自身について答えるもう 1 つのリスナー:

| パス | 答えるもの |
| --- | --- |
| `/metrics` | Prometheus テキスト形式。マッチしたルート別のリクエスト数・レイテンシヒストグラム・レスポンスサイズ、処理中リクエスト数、コネクションプールの飽和、Go ランタイム、スコープ別のレート制限拒否数、送信メールの結果別件数 |
| `/debug/pprof/*` | 稼働中プロセスの CPU・ヒープ・goroutine プロファイル |
| `/health/detail` | 依存先ごとの詳細レポート（DB・オブジェクトストレージ・メールリレー）。失敗したプローブが返したエラーを含む。503 を返すのは**必須**依存が落ちたときだけで、メールリレーが不通なだけでロードバランサから外されることはない |

遮断は二重で、どちらも転送ヘッダを見ない。既定で loopback にバインドし、さらに直接の TCP ピアを `TC_METRICS_ALLOWED_CIDRS` と照合する。リスト外からのリクエストには 403 ではなく 404 を返す — 「許可されていない」という応答はエンドポイントの存在を裏付けてしまうため。**このポートの前にリバースプロキシの設定を置かないこと。** プロキシ経由では全リクエストがプロキシのアドレスから届くので、許可エントリ 1 つでインターネット全体を通すことになる。

`compose.prod.yml` はホストの `127.0.0.1:9464` に公開する。同じ compose ネットワーク上のスクレイパからは `api:9464` で到達できる。

```bash
curl -s 127.0.0.1:9464/metrics | head
curl -s 127.0.0.1:9464/health/detail | jq
go tool pprof http://127.0.0.1:9464/debug/pprof/profile?seconds=30
```

公開側の `/health` はロードバランサが必要とするもののまま。軽く、認証不要で、どの依存先が不調かは語らない。

**アクセスログは URL ではなくルートを記録する。** 共有トークンはパスの一部であり、パスワード再設定はトークンをクエリに載せる。素直にアクセスログを取ると、最も長く保存され最も広く読まれる場所に有効な資格情報を書き残すことになる。各行が持つのは、マッチしたルートパターン・ステータス・所要時間・レスポンスサイズ・クライアント IP・リクエスト識別子。識別子は `X-Request-Id` として返り、そのリクエストが出す他のすべてのログ行にも付くので、1 つの値で一連の流れを拾える。呼び出し側が指定した識別子を採用するのは `TC_TRUSTED_PROXIES` に載っているホップからのときだけ。

web サービスの nginx も同じ規則に従う。アクセスログに載るのはクエリ文字列を除いたパスだけで、`/share/`・`/embed/`・`/invites/` の URL はトークンの区間を置き換え、`Referer` は記録しない。失敗時にだけ書かれるエラーログには、失敗したリクエストのリクエスト行と `Referer` がそのまま載る。

## テスト

E2E はテストごとに独立ユーザー＋カレンダーを生成（テナント分離）し、実 MySQL に対して並列実行する。

```bash
make db-up                      # TC_DB_PORT（既定 33306）で MySQL 起動
make test-e2e                   # TC_TEST_INTEGRATION=1 で実行
```

テストは `127.0.0.1:${TC_DB_PORT:-33306}` に接続する。CI も同じなので、既定以外の `TC_DB_PORT` を使うなら両方のコマンドで export しておく。

CI（`.github/workflows/ci.yml`）では Web（Biome / typecheck / Vitest）と API（build / vet / E2E）を実行する。

## DB マイグレーション / コード生成

- スキーマは 2 層構成。`sql/core/` は複数プロダクトが同じ DB に書き合うための共有スキーマ契約のコピー（編集不可、`sql/core/UPSTREAM.json` で上流に固定）、`sql/time/` が本アプリ固有のテーブル。
- `sql/build-schema.sh` が core → time の順に結合して `sql/schema.sql` を生成し、`make db-apply` が適用する。
- `make test-conformance` で、生成したスキーマが契約を満たしているかを検証する。`make check-core` は vendor したコピーが未編集かつ最新かを確認する。
- クエリは `sql/queries/*.sql` に書き、`make sqlc` で `apps/api/internal/db/generated/` に Go コードを生成する。

## デプロイ

`compose.prod.yml` が MySQL・オブジェクトストレージ・API・ビルド済み SPA を 1 台のホスト上でまとめて動かす。イメージは pull せずその場でビルドする——SPA は API のオリジンをバンドルに焼き込むため、2 つのデプロイ先に対して正しいビルド済みイメージは存在しない。

ホスト名 1 つ、証明書 1 枚、公開するポートはそれだけ。入口は web サービスひとつで、そこが TLS を終端し、SPA を配信し、`/api` を compose ネットワーク越しに API へ渡す。API もオブジェクトストレージもインターネットからの経路を必要としないし、与えてもいない——添付・アルバム写真・アバターは API が中継する。ブラウザには API 自身へ戻る短命の署名付き URL を渡し、バイトはその上をコピーして流す。

証明書はスタックを起動する前に存在している必要がある。証明書のない server ブロックを nginx が読み込まないためだ。ポート 80 を空けた状態で先に取得する。

```sh
mkdir -p acme
certbot certonly --standalone -d app.example.com
```

更新は稼働中のサーバが `acme/` から応答するので、止めるものは何もない。ただし nginx は言われるまで古い証明書を持ち続ける。

```sh
certbot renew --webroot -w /path/to/acme \
  --deploy-hook 'docker compose -f /path/to/compose.prod.yml exec web nginx -s reload'
```

`TC_API_PUBLIC_URL` には `/api` の prefix が要る（`https://app.example.com/api`）。OAuth のコールバックが戻ってくる先であり、blob URL が指し戻す先であり、レスポンスの `$schema` リンクを組み立てる元でもあるため。`TC_TRUSTED_PROXIES` の既定は Docker が compose ネットワークに割り当てるレンジ `172.16.0.0/12`。ネットワークがその外に置かれた場合は自分でレンジを入れること。全クライアントが nginx を通って届くので、合っていないとレート制限が 1 バケットに潰れる。

TLS を別の場所で終端したい場合——ホスト上のリバースプロキシ、あるいはトンネル——は、`web` サービスから TLS テンプレートのマウントと証明書の 2 つのボリュームを外し、`127.0.0.1:8081:80` を publish する。イメージは平文 HTTP で応答し、`/api` は同じように自分で API へ流すので、ブラウザから見た構成は上と変わらない。ホスト側のプロキシはその 1 ポートだけを向けばよい。

設定は `compose.prod.yml` と同じ場所の `.env` に置く。`TC_DB_ROOT_PASSWORD`・`TC_DB_PASSWORD`・`TC_JWT_SECRET`（`openssl rand -hex 32`）・`TC_WEB_URL`・`TC_API_PUBLIC_URL`・`TC_PUBLIC_HOST`・`TC_S3_ACCESS_KEY`・`TC_S3_SECRET_KEY`・`TC_SMTP_FROM` には安全な既定値がなく、未設定なら Compose が該当する変数名を出して停止する。`TC_SMTP_HOST` がなければ API が起動を拒否する（`TC_ALLOW_CONSOLE_MAILER=true` ならメールを配送せずログに書いて起動する）。上の表のその他の変数も同じ既定値のまま渡る。例外は次のとおり。

- `TC_CORS_ALLOWED_ORIGINS` は常に `TC_WEB_URL` になる。`.env` に書いた値は無視される。
- `TC_PORT` は渡さない。API はコンテナ内で 8080 を待ち受け、ホスト側の loopback ポートは `TC_API_PORT`（既定 8080）で決める。
- `TC_METRICS_ADDR` の既定は `:9464`。コンテナはホストの loopback にバインドできないため。`127.0.0.1` に閉じているのは公開側のポート（`TC_METRICS_PORT`、既定 9464）。
- `TC_ENV` は常に `production`、`TC_DB_DSN` は `TC_DB_USER`・`TC_DB_PASSWORD`・`TC_DB_NAME` から組み立てられる。
`TC_SECRETS_KEY` は任意で、設定しない場合、管理画面は OAuth のクライアントシークレットを平文で保存する代わりに保存自体を拒否する。

web サービスが返すページにはすべて、フレーム埋め込みを禁じる Content-Security-Policy、`nosniff`、`Referrer-Policy: no-referrer` が付き、TLS を自分で終端する場合は HSTS も付く。例外は `/embed/*` で、どのサイトからでもフレームに入れられる。ポリシーが許すスクリプトと接続先は配信元と同じオリジンだけなので、別オリジンで API を配信する場合は `apps/web/nginx/security-headers.conf` の `connect-src` を広げる。

```sh
docker compose -f compose.prod.yml up -d --build
docker compose -f compose.prod.yml run --rm --entrypoint /createuser api \
  -email you@example.com -password 'choose-a-long-one' -name "Your Name" -admin
```

スキーマは MySQL のボリュームを最初に作ったときに適用される。`sql/schema.sql` は全テーブルを DROP してから作り直すので、データの入った DB には流し直さない。スキーマの変更を既存の DB に当てる方法は「[既存環境の更新](#既存環境の更新)」を参照。

永続データは `mysql_data` と `minio_data` の 2 つのボリュームに入っている。バックアップは必ず両方を揃えて取る——添付ファイルは片方の行がもう片方のオブジェクトを指す形で成立している。

### 既存環境の更新

先に上記のとおり 2 つのボリュームをバックアップする。そのうえで、今のリリースから移行先までの各リリースについて `sql/upgrades/from-<そのリリース>.sql` を古い順に当て、それから再ビルドする。

```sh
docker compose -f compose.prod.yml stop api
docker compose -f compose.prod.yml exec -T mysql \
  sh -c 'exec mysql -uroot -p"$MYSQL_ROOT_PASSWORD" "$MYSQL_DATABASE"' \
  < sql/upgrades/from-v0.1.0.sql \
  && docker compose -f compose.prod.yml up -d --build
```

パスワードとデータベース名はコンテナの中で、`.env` から渡された値として読まれるので、ホスト側で export する必要はない。再ビルドは更新が成功したときだけ走る。更新用の SQL はデータを残す。旧版の API は先に止めること。更新が済んだあとに旧版が削除した行は、確認待ちのアップロードに見えてしまう。ファイルの各ステップは現状を確かめてから動くので、途中で止まったものは、新しい API がまだリクエストを処理していない限り、もう一度流せる。

v0.1.0 から上げる場合は、配備の形も変わっている。

- web サービスが TLS を終端し、`127.0.0.1:8081` の代わりに 80 と 443 を公開する。起動前に上記の手順で証明書を取得するか、ホスト側のプロキシを使い続けるなら「TLS を別の場所で終端したい場合」の段落に従う。
- 証明書を発行するホスト名 `TC_PUBLIC_HOST` が必須になった。
- Compose は `TC_SMTP_HOST` を必須にしなくなった。ただし API は、`TC_ALLOW_CONSOLE_MAILER=true` が無い限り、これが無いと起動しない。
- `TC_TRUSTED_PROXIES` の既定値が、Docker が compose のネットワークに割り当てる範囲 `172.16.0.0/12` になった。

### オブジェクトストレージ

同梱のストレージは単一ノード・単一ドライブで動く。erasure coding もバージョニングもレプリケーションもない。耐久性は下のボリュームと上記のバックアップに依存する——データベースと同じ足場の上にある。

上流のリポジトリは 2026 年 4 月にアーカイブされた。`compose.prod.yml` が固定しているのは最後に公開されたイメージ `RELEASE.2025-09-07T16-13-09Z` である。最後の**ソース**リリースはその 1 か月後だが、対応するイメージはついに公開されなかったので、そちらのタグを指定するとスタックが起動しない。

メンテナンスの止まったストレージを使い続けるのは見落としではなく判断による。プライベートネットワークにしか応答しないので、もう修正されないコードは認証を持たない呼び出し元が到達できるコードではない。加えて、イメージ化されなかった最終リリースの修正はサービスアカウントと STS に関するもので、このデプロイはどちらも使っていない。この釣り合いが受け入れられないなら、`TC_S3_*` を任意の S3 互換エンドポイントに向ければよく、設定より上は何も変わらない——どちらにせよストレージに触るのは API で、ブラウザではない。

```sh
docker compose -f compose.prod.yml exec -T mysql \
  sh -c 'exec mysqldump -uroot -p"$MYSQL_ROOT_PASSWORD" --single-transaction "$MYSQL_DATABASE"' \
  > backup.sql
```

## ライセンス

[AGPL-3.0](./LICENSE) — 自己ホスト・改変・再配布は自由。ネットワーク経由で提供する場合はソースの公開が必要。

## 免責事項・商標について

本プロジェクトは独立した独自プロジェクトです。TimeTree の共有カレンダー体験に**着想を得て**いますが、コード・UI・UX はすべて独自実装であり、第三者のソース・素材・デザインを流用していません。

"TimeTree"、"Google Calendar" その他の製品名は各社の商標であり、本ドキュメントでは説明のための参照としてのみ使用しています。本プロジェクトはこれらの企業と**提携・公認・後援の関係には一切ありません**。
