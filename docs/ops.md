# Эксплуатация: локально, Codespaces, VPS, бэкапы

Как запустить проект у себя, дать его тестировщикам в GitHub Codespaces и развернуть на VPS в России.
Переменные окружения и раскладка репозитория описаны в [`contracts/layout.md`](contracts/layout.md).

## Как это устроено

```
браузер ──HTTPS──▶ Caddy (TLS, 80/443) ──▶ api (Go: /api/v1, /healthz, /r/{id}, /admin + статика) ──▶ Postgres 16
```

- Один процесс Go отдаёт и API, и статику симулятора (`index.html`, `app.js`, `features/`, …) на порту `8080`,
  поэтому браузер всегда ходит на свой же адрес.
- Caddy есть только на VPS: он получает сертификат Let's Encrypt и проксирует всё на `api:8080`.
- Postgres наружу не открыт ни на VPS, ни в Codespaces.

| Файл | Зачем |
|------|-------|
| `Makefile` | все команды: `make` без аргументов показывает список |
| `deploy/Dockerfile.api` | образ API: статический бинарник Go + фронтенд в `/srv/static`, distroless, не root |
| `deploy/compose.dev.yml` | Postgres для разработки (базы `mq` и `mq_test`), API — по профилю `api` |
| `deploy/compose.prod.yml` | VPS: caddy + api + postgres, лимиты, перезапуск, ротация логов |
| `deploy/Caddyfile`, `deploy/caddy/` | настройки Caddy и его образ без root |
| `deploy/.env.example` | все настройки VPS с пояснениями; копия `deploy/.env` в git не попадает |
| `deploy/backup/` | `backup.sh`, `restore.sh`, systemd-юниты таймера |
| `.devcontainer/` | конфигурация Codespaces |
| `.github/workflows/ci.yml` | CI: Go-тесты на Postgres, браузерные тесты, сборка образа |

## Локальный запуск

Нужно: Docker с `docker compose`, Go 1.24, Node 22, `make`.

```sh
make dev          # Postgres в Docker + go run ./server/cmd/api → http://localhost:8080
make test         # go test ./... (база mq_test) и браузерные тесты из tests/
make db-shell     # psql в базу разработки
make db-down      # остановить Postgres (данные остаются); make db-reset — удалить и данные
```

- Postgres слушает только `127.0.0.1:5432` (пользователь и пароль `mq`/`mq` — только для разработки). Если порт
  занят: `make dev MQ_PG_PORT=5433`.
- `DATABASE_URL=postgres://mq:mq@localhost:5432/mq?sslmode=disable`, для тестов
  `TEST_DATABASE_URL=…/mq_test` — отдельная база, тесты не трогают данные `make dev`.
- Браузерным тестам нужен Chromium: один раз `cd tests && npm ci && npx playwright-core install chromium`
  (или свой браузер через `CHROMIUM_PATH=/путь/к/chrome`).
- Пока в репозитории нет `server/`, `make dev` и `make build-image` честно говорят об этом и выходят, а `make test`
  пропускает Go и запускает только браузерные тесты.
- Весь прод-стек на ноутбуке: `cp deploy/.env.example deploy/.env`, в нём `DOMAIN=localhost`, любой
  `ACME_EMAIL`, `POSTGRES_PASSWORD=$(openssl rand -hex 24)`; затем `make up-prod` и `curl -k https://localhost/healthz`
  (для `localhost` Caddy выпускает сертификат своим локальным центром, отсюда `-k`). Остановить: `make down-prod`.

## Codespaces для тестировщиков

Тестовая фаза до запуска идёт в GitHub Codespaces: там поднимаются Go, Node, Postgres и API, а наружу
открывается один адрес.

**Что важно знать**

- Codespace **засыпает после простоя**: по умолчанию через 30 минут, максимум — 4 часа (GitHub → Settings →
  Codespaces → *Default idle timeout*). Уснувший codespace не отвечает тестировщикам, пока его не разбудят
  (открыть его на github.com/codespaces) и снова не выполнят `make dev`.
- Адрес порта вида **`https://<имя-codespace>-8080.app.github.dev`**. Имя своё у каждого codespace: новый codespace —
  новый адрес.
- Чтобы тестировщики открывали ссылку **без входа в GitHub**, порт нужно сделать **Public** (по умолчанию он Private
  и требует входа под владельцем).
- Бесплатная квота личного аккаунта — **120 core-hours в месяц**, то есть около 60 часов на машине с 2 ядрами
  (плюс 15 ГБ-месяцев хранилища). Остановленный codespace ядра не тратит. Для настоящих A/B-тестов нужен
  постоянно работающий сервер — VPS ниже.

**По шагам**

1. На странице репозитория: **Code → Codespaces → Create codespace on main** (машина 2-core хватает).
   Первая сборка занимает несколько минут: ставятся зависимости тестов и Chromium (`.devcontainer/post-create.sh`).
2. В терминале codespace: `make dev`. Postgres уже запущен рядом (сервис `postgres` из
   `.devcontainer/compose.yml`, база доступна на `localhost:5432`), команда запускает API на порту 8080.
3. Вкладка **Ports** (рядом с Terminal): строка *8080 — Симулятор и API*. Правый клик → **Port Visibility →
   Public**. То же из консоли: `gh codespace ports visibility 8080:public -c <имя-codespace>`
   (имя: `gh codespace list`).
4. Скопировать адрес из колонки *Forwarded Address* (`https://<имя>-8080.app.github.dev`) и проверить его в окне
   инкогнито: должна открыться страница симулятора, а `…/healthz` — ответить `ok`.
5. Отправить ссылку тестировщикам. Перед сессией тестирования увеличить idle timeout до 4 часов, после —
   остановить codespace (**…→ Stop codespace**), чтобы не тратить квоту.

Пароль `/admin` в Codespaces задаётся секретами: GitHub → Settings → Codespaces → *Secrets*: `ADMIN_USER`,
`ADMIN_PASSWORD`, доступ — этому репозиторию. Они появляются в окружении codespace, и `make dev` передаёт их API.
`make test` и `make db-shell` в codespace работают так же, как локально.

## VPS в России

**Почему Россия.** По 152-ФЗ персональные данные граждан РФ (почта в листе ожидания) должны записываться и храниться
в базах данных на территории РФ. Поэтому база — на VPS в российском дата-центре, а бэкапы — в российском
S3-хранилище.

**Где и какой.** Selectel, Timeweb Cloud или Yandex Cloud. Хватает **2 vCPU, 2–4 ГБ RAM**, 30–40 ГБ SSD,
Ubuntu 24.04 LTS. Лимиты в `compose.prod.yml` рассчитаны на такую машину: postgres 1 ГБ, api 512 МБ, caddy 256 МБ.

### Первый деплой

1. **Сервер.** Создать VPS с Ubuntu 24.04, вход по SSH-ключу. Затем на сервере:

   ```sh
   sudo timedatectl set-timezone Europe/Moscow
   sudo apt update && sudo apt -y upgrade && sudo apt -y install git make
   curl -fsSL https://get.docker.com | sudo sh          # Docker Engine + compose plugin
   ```

   Если Docker Hub с этого сервера не отдаёт образы, прописать зеркало в `/etc/docker/daemon.json`
   (`{"registry-mirrors": ["https://mirror.gcr.io"]}` или зеркало своего провайдера) и выполнить
   `sudo systemctl restart docker`.

2. **Файрвол** — см. раздел «Файрвол» ниже: открыть только 22, 80 и 443.

3. **DNS.** У регистратора домена: запись `A` (и `AAAA`, если есть IPv6) для домена → IP сервера. Проверить:
   `dig +short example.ru` показывает IP сервера. Без этого Caddy не получит сертификат.

4. **Код.**

   ```sh
   sudo git clone https://github.com/Haleralex/ballistic-mannequin.git /opt/ballistic-mannequin
   cd /opt/ballistic-mannequin
   ```

   Пути в systemd-юнитах бэкапа рассчитаны именно на `/opt/ballistic-mannequin`.

5. **Настройки.**

   ```sh
   sudo cp deploy/.env.example deploy/.env && sudo chmod 600 deploy/.env
   openssl rand -hex 24        # → POSTGRES_PASSWORD (только буквы и цифры: пароль попадает в URL)
   openssl rand -base64 18     # → ADMIN_PASSWORD
   sudo nano deploy/.env       # DOMAIN, ACME_EMAIL, POSTGRES_PASSWORD, ADMIN_USER, ADMIN_PASSWORD, бэкапы
   ```

   Формат файла — простые строки `КЛЮЧ=значение` без кавычек: его читают и `docker compose`, и systemd.

6. **Запуск.**

   ```sh
   sudo make up-prod
   ```

   Собираются образы API (с `BUILD` = короткий sha коммита) и Caddy, поднимаются postgres → api → caddy,
   команда ждёт, пока все станут healthy, и показывает их состояние. Первый запуск: Caddy за несколько секунд
   получает сертификат Let's Encrypt (порт 80 должен быть открыт).

7. **Проверка.**

   ```sh
   curl -fsS https://example.ru/healthz          # ok
   curl -fsSI https://example.ru/ | head -1      # HTTP/2 200
   sudo make ps-prod                             # все три контейнера (healthy)
   ```

   Открыть `https://example.ru/admin` — спросит логин и пароль из `.env`.

8. **Бэкапы** — включить таймер (раздел «Бэкапы» ниже) и сразу проверить восстановление.

### Обновление

```sh
cd /opt/ballistic-mannequin
sudo git pull
sudo make up-prod        # пересобирает и перезапускает только то, что изменилось
```

Миграции базы встроены в бинарник API (`server/migrations`) и применяются сервером, отдельной команды для них нет.
Перед обновлением с миграциями стоит снять бэкап: `sudo make backup`.

**Откат:** `sudo git checkout <предыдущий-sha> && sudo make up-prod` (образ прошлой версии, как правило, ещё в
кеше Docker). Если новая версия уже изменила схему базы — восстановить бэкап, снятый перед обновлением.
Старые образы удаляются командой `sudo docker image prune`.

Изменить настройки: поправить `deploy/.env` и выполнить `sudo make up-prod` — пересоздадутся контейнеры, чьи
настройки изменились. Исключение — `POSTGRES_PASSWORD`: в уже созданной базе он сам не меняется, сначала
`sudo make db-shell-prod` → `ALTER USER mq PASSWORD '<новый>';`, потом правка `.env` и `make up-prod`.

## Бэкапы и восстановление

`deploy/backup/backup.sh` делает `pg_dump -Fc` внутри контейнера postgres в файл
`$BACKUP_DIR/mq-<дата-время UTC>.dump` (по умолчанию `/var/backups/mq`), проверяет, что дамп читается
(`pg_restore --list`), удаляет локальные дампы старше `BACKUP_KEEP_DAYS` дней (14) и, если настроено, копирует
новый дамп в S3-совместимое хранилище. Настройки — в `deploy/.env`, раздел «backups».

**Включить ежедневный бэкап** (около 03:30 по времени сервера):

```sh
sudo cp deploy/backup/mq-backup.service deploy/backup/mq-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now mq-backup.timer
systemctl list-timers mq-backup.timer           # когда следующий запуск
sudo systemctl start mq-backup.service          # снять бэкап прямо сейчас (или: sudo make backup)
journalctl -u mq-backup.service -n 50           # что было при последнем запуске
```

**Копия вне сервера (S3 в России).** Два способа, достаточно одного.

- *rclone* (`sudo apt install rclone`). Конфиг `/root/.config/rclone/rclone.conf`:

  ```ini
  [selectel]
  type = s3
  provider = Other
  access_key_id = …
  secret_access_key = …
  endpoint = https://s3.ru-1.storage.selcloud.ru
  region = ru-1

  [yandex]
  type = s3
  provider = Other
  access_key_id = …
  secret_access_key = …
  endpoint = https://storage.yandexcloud.net
  region = ru-central1
  ```

  В `deploy/.env`: `BACKUP_RCLONE_REMOTE=selectel:mq-backups/db`. Проверка: `sudo rclone ls selectel:mq-backups`.

- *aws CLI*: в `deploy/.env` — `BACKUP_S3_URI=s3://mq-backups/db`, `BACKUP_S3_ENDPOINT` (Selectel
  `https://s3.ru-1.storage.selcloud.ru`, Yandex `https://storage.yandexcloud.net`), `AWS_ACCESS_KEY_ID`,
  `AWS_SECRET_ACCESS_KEY`, `AWS_DEFAULT_REGION`.

Ключи — сервисного аккаунта с доступом только к этому бакету. Срок хранения копий в бакете задаётся правилом
жизненного цикла (lifecycle) в панели провайдера, например удалять объекты старше 60 дней.

**Проверить, что бэкап восстанавливается** (пункт чек-листа запуска), не трогая рабочую базу:

```sh
sudo make restore-check FILE=/var/backups/mq/mq-2026-11-01T033012Z.dump
```

Дамп разворачивается в отдельную чистую базу `mq_restore_check`, в конце печатается число строк в каждой таблице —
сравнить с рабочей базой. Удалить проверочную базу: `sudo make db-shell-prod` → `DROP DATABASE mq_restore_check;`.

**Восстановить рабочую базу** (после сбоя или переезда на новый сервер):

```sh
sudo rclone copy selectel:mq-backups/db/mq-2026-11-01T033012Z.dump /var/backups/mq/   # если файла нет на сервере
sudo docker compose -f deploy/compose.prod.yml stop api caddy
sudo deploy/backup/restore.sh --fresh /var/backups/mq/mq-2026-11-01T033012Z.dump        # спросит имя базы
sudo make up-prod
```

`--fresh` удаляет базу `mq` и создаёт её заново из дампа. Без `--fresh` дамп накатывается поверх существующей
базы (`pg_restore --clean --if-exists`). Сам `pg_restore` идёт одной транзакцией: если он упал, без `--fresh` база
остаётся как была, а с `--fresh` — пустой (тогда исправить причину и повторить).

## Файрвол

Снаружи нужны только **22** (SSH), **80** (HTTP: Let's Encrypt и редирект на HTTPS) и **443** (HTTPS, включая
HTTP/3 по UDP).

```sh
sudo ufw default deny incoming
sudo ufw default allow outgoing
sudo ufw allow 22/tcp
sudo ufw allow 80/tcp
sudo ufw allow 443/tcp
sudo ufw allow 443/udp
sudo ufw enable
sudo ufw status verbose
```

Docker открывает опубликованные порты контейнеров мимо ufw (через свои правила iptables), поэтому важно, что в
`compose.prod.yml` порты публикует только Caddy (80/443), а у Postgres и API их нет. Если у провайдера есть
облачный файрвол (группы безопасности в Yandex Cloud, файрвол в панели Selectel/Timeweb), задать в нём те же
правила. SSH: вход только по ключу (`PasswordAuthentication no` в `/etc/ssh/sshd_config`).

## Где логи

| Что | Как смотреть |
|-----|--------------|
| Все сервисы | `sudo make logs-prod` (последние 200 строк и дальше в реальном времени) |
| Только API / Caddy / Postgres | `sudo make logs-prod S=api` (`S=caddy`, `S=postgres`) |
| Журнал запросов | лог Caddy, по строке JSON на запрос: `sudo make logs-prod S=caddy` |
| Медленные запросы к базе | лог Postgres: запросы дольше 500 мс (`log_min_duration_statement`) |
| Состояние и healthcheck | `sudo make ps-prod`; подробно: `sudo docker inspect --format '{{json .State.Health}}' mq-api-1` |
| Бэкапы | `journalctl -u mq-backup.service` |

Логи контейнеров лежат в `/var/lib/docker/containers/<id>/<id>-json.log` и ротируются: до 5 файлов по 10 МБ на
контейнер, так что диск они не переполнят.

## Что инфраструктура ждёт от сервера

Образ и compose-файлы проверены на заглушке сервера. Настоящий `server/` (команда B) должен:

- собираться командой `go build ./cmd/api` в модуле `server/` без CGO (образ статический, distroless) и иметь
  `go.sum`, если есть зависимости;
- слушать `PORT` (8080) на всех интерфейсах, читать `DATABASE_URL`, `STATIC_DIR`, `BUILD` и остальные переменные
  из `contracts/layout.md` — флаги командной строки не передаются;
- отвечать `200` на `GET /healthz`, когда база доступна: по нему Docker решает, что API здоров, и только после
  этого запускает Caddy;
- применять миграции сам при старте (контейнер один, отдельного шага миграций нет) и ничего не писать на диск,
  кроме `/tmp`: корневая файловая система контейнера только для чтения;
- в `go test` брать базу из `TEST_DATABASE_URL` (база `mq_test`, в CI — отдельный контейнер Postgres 16).

После появления кода проверить: `make dev` и `make test` локально, `make up-prod` с `DOMAIN=localhost`,
зелёный CI на ветке.
