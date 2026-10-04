#!/bin/sh
# Cópia diária do banco (contêiner postgres-backup).
#
# - Guarda as diárias por BACKUP_RETENTION_DAYS e a de domingo (semanal) por
#   BACKUP_WEEKLY_KEEP semanas.
# - Antes de copiar, garante espaço: as cópias não podem encher o disco.
#   Faltou? Apaga as mais antigas (ficam sempre as 2 mais novas) e, se ainda
#   faltar, pula a cópia e avisa no log — o painel mostra o backup atrasado.
# - Com BACKUP_S3_BUCKET, manda cada cópia criptografada (rclone crypt, com
#   BACKUP_ENCRYPTION_PASSPHRASE) para um armazenamento S3 compatível fora da
#   VPS (Backblaze B2, Cloudflare R2, AWS S3...), guardada lá por
#   BACKUP_OFFSITE_KEEP_DAYS dias. O resultado vai para offsite-status.json,
#   que o painel mostra em Diagnóstico → Servidor.
set -u

DIR=${BACKUP_DIR:-/backups}
DAILY_DAYS=${BACKUP_RETENTION_DAYS:-7}
WEEKLY_KEEP=${BACKUP_WEEKLY_KEEP:-4}
MIN_FREE_MB=${BACKUP_MIN_FREE_MB:-2048}
OFFSITE_KEEP_DAYS=${BACKUP_OFFSITE_KEEP_DAYS:-30}
STATUS="$DIR/offsite-status.json"
RETRY=3600

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }

# offsite_status <configurado> <ok> <arquivo> <erro>: escrita atômica.
offsite_status() {
    error=$(printf '%s' "$4" | tr -d '"\\' | tr '\n' ' ' | cut -c1-300)
    printf '{"configured":%s,"ok":%s,"at":"%s","file":"%s","error":"%s"}\n' \
        "$1" "$2" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$3" "$error" >"$STATUS.tmp" &&
        mv "$STATUS.tmp" "$STATUS"
}

free_mb() { df -Pm "$DIR" | awk 'NR == 2 { print $4 }'; }

newest_mb() {
    newest=$(ls -t "$DIR"/tracker-*.dump 2>/dev/null | head -n 1)
    if [ -n "$newest" ]; then du -m "$newest" | cut -f1; else echo 0; fi
}

# make_room apaga as cópias mais antigas até caber a próxima (1,5× a última
# mais BACKUP_MIN_FREE_MB de folga para o banco).
make_room() {
    need=$(($(newest_mb) * 3 / 2 + MIN_FREE_MB))
    while [ "$(free_mb)" -lt "$need" ]; do
        [ "$(ls "$DIR"/tracker-*.dump 2>/dev/null | wc -l)" -le 2 ] && return 1
        oldest=$(ls -tr "$DIR"/tracker-*.dump | head -n 1)
        log "pouco espaço em disco: apagando a cópia mais antiga, $(basename "$oldest")"
        rm -f "$oldest"
    done
}

prune() {
    find "$DIR" -maxdepth 1 -type f -name 'tracker-*.dump' ! -name 'tracker-weekly-*' \
        -mtime "+$DAILY_DAYS" -delete
    find "$DIR" -maxdepth 1 -type f -name 'tracker-weekly-*.dump' -mtime "+$((WEEKLY_KEEP * 7))" -delete
}

offsite_ready=false

setup_offsite() {
    if [ -z "${BACKUP_S3_BUCKET:-}" ]; then
        offsite_status false false "" ""
        return
    fi
    if [ -z "${BACKUP_ENCRYPTION_PASSPHRASE:-}" ]; then
        log "BACKUP_S3_BUCKET sem BACKUP_ENCRYPTION_PASSPHRASE: a cópia não sai da VPS sem criptografia"
        offsite_status true false "" "defina BACKUP_ENCRYPTION_PASSPHRASE"
        return
    fi
    if ! command -v rclone >/dev/null 2>&1 && ! apk add --no-cache rclone >/dev/null 2>&1; then
        log "não foi possível instalar o rclone; a cópia externa fica para a próxima"
        offsite_status true false "" "rclone indisponível"
        return
    fi
    export RCLONE_CONFIG_OFFSITE_TYPE=s3
    export RCLONE_CONFIG_OFFSITE_PROVIDER="${BACKUP_S3_PROVIDER:-Other}"
    export RCLONE_CONFIG_OFFSITE_ENDPOINT="${BACKUP_S3_ENDPOINT:-}"
    export RCLONE_CONFIG_OFFSITE_REGION="${BACKUP_S3_REGION:-}"
    export RCLONE_CONFIG_OFFSITE_ACCESS_KEY_ID="${BACKUP_S3_ACCESS_KEY_ID:-}"
    export RCLONE_CONFIG_OFFSITE_SECRET_ACCESS_KEY="${BACKUP_S3_SECRET_ACCESS_KEY:-}"
    # Chave restrita ao bucket (B2/R2) não pode listar nem criar buckets.
    export RCLONE_CONFIG_OFFSITE_NO_CHECK_BUCKET=true
    export RCLONE_CONFIG_SECURE_TYPE=crypt
    export RCLONE_CONFIG_SECURE_REMOTE="offsite:${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX:-farbo}"
    # Conteúdo cifrado; o nome (só a data) fica legível para achar a cópia.
    export RCLONE_CONFIG_SECURE_FILENAME_ENCRYPTION=off
    export RCLONE_CONFIG_SECURE_DIRECTORY_NAME_ENCRYPTION=false
    RCLONE_CONFIG_SECURE_PASSWORD=$(rclone obscure "$BACKUP_ENCRYPTION_PASSPHRASE")
    export RCLONE_CONFIG_SECURE_PASSWORD
    offsite_ready=true
}

send_offsite() {
    [ "$offsite_ready" = true ] || return 0
    name=$(basename "$1")
    if out=$(rclone copyto "$1" "secure:$name" --retries 3 --low-level-retries 10 2>&1); then
        rclone delete secure: --min-age "${OFFSITE_KEEP_DAYS}d" >/dev/null 2>&1 ||
            log "falha ao apagar as cópias antigas fora da VPS"
        offsite_status true true "$name" ""
        log "cópia enviada para fora da VPS: $name"
    else
        offsite_status true false "$name" "$(printf '%s' "$out" | tail -n 1)"
        log "ERRO: a cópia não foi enviada para fora da VPS: $(printf '%s' "$out" | tail -n 3)"
    fi
}

# Restauração: "listar" mostra as cópias fora da VPS; "baixar <arquivo>"
# traz uma delas, já descriptografada, para a pasta dos backups.
case "${1:-}" in
listar | baixar)
    setup_offsite
    if [ "$offsite_ready" != true ]; then
        echo "cópia externa não configurada (BACKUP_S3_* e BACKUP_ENCRYPTION_PASSPHRASE)" >&2
        exit 1
    fi
    [ "$1" = listar ] && exec rclone ls secure:
    [ -n "${2:-}" ] || {
        echo "uso: postgres-backup.sh baixar <arquivo>" >&2
        exit 2
    }
    exec rclone copy "secure:$2" "$DIR/"
    ;;
esac

# Sobra de uma cópia interrompida (o contêiner caiu no meio).
rm -f "$DIR"/tracker-*.dump.partial

while true; do
    [ "$offsite_ready" = true ] || setup_offsite
    prune
    if ! make_room; then
        log "ERRO: sem espaço em disco para a cópia do banco (precisa de ${need} MB livres); nova tentativa em 1 h"
        sleep "$RETRY"
        continue
    fi

    kind=tracker
    [ "$(date -u +%u)" = 7 ] && kind=tracker-weekly
    backup="$DIR/${kind}-$(date -u +%Y%m%dT%H%M%SZ).dump"
    if ! pg_dump --format=custom --file="$backup.partial"; then
        rm -f "$backup.partial"
        log "ERRO: o pg_dump falhou; nova tentativa em 1 h"
        sleep "$RETRY"
        continue
    fi
    mv "$backup.partial" "$backup"
    log "cópia do banco: $(basename "$backup") ($(du -h "$backup" | cut -f1))"
    send_offsite "$backup"
    sleep 86400
done
