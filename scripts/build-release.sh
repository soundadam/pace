#!/bin/sh
set -eu

ROOT=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
  echo "usage: $0 VERSION [GIT_REF]" >&2
  exit 1
fi

version=${1#v}
ref=${2:-"v$version"}
archive="$ROOT/dist/pace-$version.tar.gz"
formula="$ROOT/dist/Formula/pace.rb"

validate_version() {
  candidate=$1
  major=${candidate%%.*}
  remainder=${candidate#*.}
  if [ "$remainder" = "$candidate" ]; then
    return 1
  fi
  minor=${remainder%%.*}
  patch=${remainder#*.}
  if [ "$patch" = "$remainder" ]; then
    return 1
  fi
  case "$patch" in
    *.*) return 1 ;;
  esac

  for component in "$major" "$minor" "$patch"; do
    case "$component" in
      ""|*[!0-9]*) return 1 ;;
      0) ;;
      0*) return 1 ;;
      *) ;;
    esac
  done
}

if ! validate_version "$version"; then
  echo "build-release: VERSION must be numeric major.minor.patch without leading zeros" >&2
  exit 1
fi

ensure_directory() {
  directory=$1
  if [ -L "$directory" ]; then
    echo "build-release: output directory must not be a symbolic link: $directory" >&2
    exit 1
  fi
  if [ -d "$directory" ]; then
    return
  fi
  if ! mkdir -p "$directory" 2>/dev/null || [ -L "$directory" ] || [ ! -d "$directory" ]; then
    echo "build-release: cannot create directory $directory" >&2
    exit 1
  fi
}

commit=$(git -C "$ROOT" rev-parse --verify "$ref^{commit}")
ensure_directory "$ROOT/dist"
ensure_directory "$ROOT/dist/Formula"

temporary_tar="$ROOT/dist/.pace-$version.tar.$$"
temporary_archive="$archive.tmp.$$"
temporary_formula="$ROOT/dist/Formula/.pace.rb.$$"
backup_archive="$archive.backup.$$"
backup_formula="$formula.backup.$$"
release_lock="$ROOT/dist/.pace-release.lock"
stale_release_lock="$release_lock.stale.$$"
release_lock_candidate=
archive_backup_expected=0
formula_backup_expected=0
publishing=0
publication_committed=0
release_lock_acquired=0
stale_release_lock_owned=0
cleanup() {
  status=$?
  cleanup_failed=0
  trap - EXIT HUP INT TERM

  if [ "$publication_committed" -eq 0 ]; then
    if [ "$publishing" -eq 1 ]; then
      rm -f "$archive" "$formula" || cleanup_failed=1
    fi

    if [ "$archive_backup_expected" -eq 1 ] && \
      { [ -e "$backup_archive" ] || [ -L "$backup_archive" ]; }; then
      rm -f "$archive" || cleanup_failed=1
      mv "$backup_archive" "$archive" || cleanup_failed=1
    fi
    if [ "$formula_backup_expected" -eq 1 ] && \
      { [ -e "$backup_formula" ] || [ -L "$backup_formula" ]; }; then
      rm -f "$formula" || cleanup_failed=1
      mv "$backup_formula" "$formula" || cleanup_failed=1
    fi
  else
    rm -f "$backup_archive" "$backup_formula" || cleanup_failed=1
  fi

  rm -f \
    "$temporary_tar" \
    "$temporary_archive" \
    "$temporary_formula" || cleanup_failed=1

  if [ "$release_lock_acquired" -eq 1 ]; then
    if [ -n "$release_lock_candidate" ] && \
      [ -f "$release_lock" ] && [ ! -L "$release_lock" ] && \
      [ -f "$release_lock_candidate" ] && [ ! -L "$release_lock_candidate" ] && \
      [ "$release_lock" -ef "$release_lock_candidate" ]; then
      rm -f "$release_lock" "$release_lock_candidate" || cleanup_failed=1
      release_lock_candidate=
    else
      cleanup_failed=1
    fi
  elif [ -n "$release_lock_candidate" ]; then
    rm -f "$release_lock_candidate" || cleanup_failed=1
    release_lock_candidate=
  fi
  if [ "$stale_release_lock_owned" -eq 1 ]; then
    remove_release_lock_path "$stale_release_lock" || cleanup_failed=1
  fi

  if [ "$cleanup_failed" -ne 0 ] && [ "$status" -eq 0 ]; then
    status=1
  fi
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

read_release_lock_identity() {
  lock_path=$1
  lock_owner=
  lock_owner_start=

  if [ -L "$lock_path" ]; then
    return 1
  fi
  if [ -f "$lock_path" ]; then
    {
      IFS= read -r lock_owner || return 1
      IFS= read -r lock_owner_start || return 1
      if IFS= read -r _extra_line; then
        return 1
      fi
    } < "$lock_path"
  elif [ -d "$lock_path" ]; then
    if [ ! -r "$lock_path/pid" ] || [ ! -r "$lock_path/start" ]; then
      return 1
    fi
    IFS= read -r lock_owner < "$lock_path/pid" || return 1
    IFS= read -r lock_owner_start < "$lock_path/start" || return 1
  else
    return 1
  fi

  case "$lock_owner" in
    ""|*[!0-9]*) return 1 ;;
    *) ;;
  esac
  case "$lock_owner_start" in
    "") return 1 ;;
    *) ;;
  esac
}

get_process_start_identity() {
  process_id=$1
  process_start=$(LC_ALL=C ps -p "$process_id" -o lstart= 2>/dev/null) || return 1
  process_start=$(printf '%s\n' "$process_start" | awk '{$1=$1; print}')
  case "$process_start" in
    ""|*'
'*) return 1 ;;
    *) printf '%s\n' "$process_start" ;;
  esac
}

remove_release_lock_path() {
  lock_path=$1
  if [ -L "$lock_path" ]; then
    return 1
  fi
  if [ -f "$lock_path" ]; then
    rm -f "$lock_path"
    return
  fi
  if [ -d "$lock_path" ]; then
    rm -f "$lock_path/pid" "$lock_path/start" || return 1
    rmdir "$lock_path"
    return
  fi
  return 1
}

remove_orphan_lock_candidates() {
  recovered_lock=$1
  for candidate in "$ROOT/dist"/.pace-release.lock.owner.*; do
    if [ -f "$candidate" ] && [ ! -L "$candidate" ] && [ "$candidate" -ef "$recovered_lock" ]; then
      rm -f "$candidate" || return 1
    fi
  done
}

publish_release_lock_candidate() {
  if [ -e "$release_lock" ] || [ -L "$release_lock" ]; then
    return 1
  fi
  if ! ln "$release_lock_candidate" "$release_lock" 2>/dev/null; then
    return 1
  fi
  if [ ! -f "$release_lock" ] || [ -L "$release_lock" ] || \
    [ ! "$release_lock" -ef "$release_lock_candidate" ]; then
    misplaced_lock="$release_lock/$(basename -- "$release_lock_candidate")"
    if [ -f "$misplaced_lock" ] && [ ! -L "$misplaced_lock" ] && \
      [ "$misplaced_lock" -ef "$release_lock_candidate" ]; then
      rm -f "$misplaced_lock" || return 1
    fi
    return 1
  fi
  release_lock_acquired=1
}

acquire_release_lock() {
  if publish_release_lock_candidate; then
    return 0
  fi

  if [ -L "$release_lock" ] || { [ ! -f "$release_lock" ] && [ ! -d "$release_lock" ]; }; then
    echo "build-release: release publication lock is not a regular file or legacy directory; inspect $release_lock" >&2
    return 1
  fi
  if ! read_release_lock_identity "$release_lock"; then
    echo "build-release: release publication lock has no valid owner process identity; inspect $release_lock" >&2
    return 1
  fi
  live_owner_start=$(get_process_start_identity "$lock_owner" || true)
  if [ -n "$live_owner_start" ] && [ "$live_owner_start" = "$lock_owner_start" ]; then
    echo "build-release: another release publication is already in progress (PID $lock_owner)" >&2
    return 1
  fi

  expected_lock_owner=$lock_owner
  expected_lock_start=$lock_owner_start
  if ! read_release_lock_identity "$release_lock"; then
    echo "build-release: release publication lock changed while checking its owner identity" >&2
    return 1
  fi
  current_lock_owner=$lock_owner
  current_lock_start=$lock_owner_start
  if [ "$current_lock_owner" != "$expected_lock_owner" ] || [ "$current_lock_start" != "$expected_lock_start" ]; then
    echo "build-release: release publication lock changed while checking its owner identity" >&2
    return 1
  fi
  if [ -e "$stale_release_lock" ] || [ -L "$stale_release_lock" ]; then
    echo "build-release: stale-lock recovery path already exists; inspect $stale_release_lock" >&2
    return 1
  fi
  if ! mv "$release_lock" "$stale_release_lock" 2>/dev/null; then
    echo "build-release: release publication lock changed while recovering stale owner PID $expected_lock_owner" >&2
    return 1
  fi
  stale_release_lock_owned=1
  if ! read_release_lock_identity "$stale_release_lock"; then
    moved_lock_owner=
    moved_lock_start=
  else
    moved_lock_owner=$lock_owner
    moved_lock_start=$lock_owner_start
  fi
  if [ "$moved_lock_owner" != "$expected_lock_owner" ] || [ "$moved_lock_start" != "$expected_lock_start" ]; then
    if [ ! -e "$release_lock" ] && [ ! -L "$release_lock" ]; then
      if mv "$stale_release_lock" "$release_lock" 2>/dev/null; then
        stale_release_lock_owned=0
      fi
    fi
    echo "build-release: recovered lock owner identity changed unexpectedly; inspect $stale_release_lock" >&2
    return 1
  fi
  remove_orphan_lock_candidates "$stale_release_lock"
  remove_release_lock_path "$stale_release_lock"
  stale_release_lock_owned=0
  if [ -n "$live_owner_start" ]; then
    echo "build-release: recovered stale release publication lock from PID $expected_lock_owner (process identity changed)" >&2
  else
    echo "build-release: recovered stale release publication lock from PID $expected_lock_owner (owner process exited)" >&2
  fi

  if ! publish_release_lock_candidate; then
    echo "build-release: another release publication acquired the lock during stale-lock recovery" >&2
    return 1
  fi
}

release_process_start=$(get_process_start_identity "$$" || true)
if [ -z "$release_process_start" ]; then
  echo "build-release: cannot determine the release publication process identity" >&2
  exit 1
fi
release_lock_candidate=$(mktemp "$ROOT/dist/.pace-release.lock.owner.XXXXXX")
if ! printf '%s\n%s\n' "$$" "$release_process_start" > "$release_lock_candidate"; then
  echo "build-release: cannot prepare the release publication lock owner" >&2
  exit 1
fi

if ! acquire_release_lock; then
  exit 1
fi

git -C "$ROOT" archive \
  --format=tar \
  --prefix="pace-$version/" \
  --output="$temporary_tar" \
  "$commit"
gzip -n -c "$temporary_tar" > "$temporary_archive"
rm -f "$temporary_tar"
chmod 0644 "$temporary_archive"

if command -v sha256sum >/dev/null 2>&1; then
  source_sha256=$(sha256sum "$temporary_archive" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  source_sha256=$(shasum -a 256 "$temporary_archive" | awk '{print $1}')
else
  echo "build-release: sha256sum or shasum is required" >&2
  exit 1
fi

"$ROOT/scripts/render-homebrew-formula.sh" "$version" "$source_sha256" "$temporary_formula"

if [ -e "$archive" ] || [ -L "$archive" ]; then
  archive_backup_expected=1
  if ! mv "$archive" "$backup_archive"; then
    echo "build-release: cannot stage the existing archive for replacement" >&2
    exit 1
  fi
fi
if [ -e "$formula" ] || [ -L "$formula" ]; then
  formula_backup_expected=1
  if ! mv "$formula" "$backup_formula"; then
    echo "build-release: cannot stage the existing Formula for replacement" >&2
    exit 1
  fi
fi

publishing=1
if ! mv "$temporary_archive" "$archive"; then
  echo "build-release: cannot publish the release archive" >&2
  exit 1
fi

if ! mv "$temporary_formula" "$formula"; then
  echo "build-release: cannot publish the Homebrew Formula" >&2
  exit 1
fi
publication_committed=1

printf 'Release archive: %s\n' "$archive"
printf 'Git commit:      %s\n' "$commit"
printf 'SHA-256:         %s\n' "$source_sha256"
