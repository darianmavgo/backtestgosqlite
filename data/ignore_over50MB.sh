#!/usr/bin/env bash
# Add every file over 50 MiB to this repo's .gitignore.
#
# Walks the working tree recursively from the repo root and skips .git.
# Paths are anchored at the repo root and escaped so gitignore treats them
# as literal names. The managed block at the bottom of .gitignore is
# rewritten on every run; rules above that block are left alone.
# Override the limit with IGNORE_MAX_BYTES (integer bytes).
#
# 50 MiB is 52428800 bytes, the size above which GitHub warns on push.
#
# Usage: ./ignore_over50MB.sh

set -euo pipefail

script_dir=$(cd "$(dirname "$0")" && pwd)
root=$(git -C "$script_dir" rev-parse --show-toplevel)
cd "$root"

max_bytes=${IGNORE_MAX_BYTES:-$((50 * 1024 * 1024))}
begin="# BEGIN ignore-over-50MB"
end="# END ignore-over-50MB"
gitignore="$root/.gitignore"
tab=$'\t'

# Repo-relative path -> a literal, root-anchored gitignore pattern.
pattern_for() {
	local rel=$1
	local out="" i c
	for ((i = 0; i < ${#rel}; i++)); do
		c=${rel:i:1}
		case $c in
		'\\' | '*' | '?' | '[' | ']' | '#' | '!' | ' ' | "$tab")
			out+="\\$c"
			;;
		*)
			out+="$c"
			;;
		esac
	done
	printf '/%s' "$out"
}

human() {
	local n=$1
	if ((n >= 1048576)); then
		awk -v n="$n" 'BEGIN { printf "%.1f MiB", n / 1048576 }'
	elif ((n >= 1024)); then
		awk -v n="$n" 'BEGIN { printf "%.0f KiB", n / 1024 }'
	else
		printf '%d B' "$n"
	fi
}

file_bytes() {
	wc -c <"$1" | tr -d '[:space:]'
}

outside=$(mktemp)
list=$(mktemp)
body=$(mktemp)
trap 'rm -f "$outside" "$list" "$body" "${outside}.trim"' EXIT

if [[ -f $gitignore ]]; then
	skip=0
	while IFS= read -r line || [[ -n $line ]]; do
		if [[ $line == "$begin" ]]; then
			skip=1
			continue
		fi
		if [[ $line == "$end" ]]; then
			skip=0
			continue
		fi
		if ((skip)); then
			continue
		fi
		printf '%s\n' "$line" >>"$outside"
	done <"$gitignore"
fi

# Keep one blank line between the hand-written rules and the managed block.
while [[ -s $outside ]]; do
	last=$(tail -n 1 "$outside")
	[[ -n $last ]] && break
	nlines=$(wc -l <"$outside" | tr -d '[:space:]')
	if ((nlines <= 1)); then
		: >"$outside"
		break
	fi
	head -n $((nlines - 1)) "$outside" >"${outside}.trim"
	mv "${outside}.trim" "$outside"
done

seen=0
already=0
while IFS= read -r -d '' file; do
	rel=${file#./}
	bytes=$(file_bytes "$file")
	pattern=$(pattern_for "$rel")
	seen=$((seen + 1))
	if [[ -s $outside ]] && grep -Fxq -- "$pattern" "$outside"; then
		already=$((already + 1))
		printf 'already listed: %s (%s)\n' "$rel" "$(human "$bytes")"
		continue
	fi
	printf '%s\n' "$pattern" >>"$list"
	printf 'ignore: %s (%s)\n' "$rel" "$(human "$bytes")"
done < <(find . \( -name .git -type d -prune \) -o \( -type f ! -path './.gitignore' -size +"${max_bytes}"c -print0 \) | sort -z)

count=0
if [[ -s $list ]]; then
	count=$(wc -l <"$list" | tr -d '[:space:]')
fi

cp "$outside" "$body"
if ((count > 0)); then
	if [[ -s $body ]]; then
		printf '\n' >>"$body"
	fi
	{
		printf '%s\n' "$begin"
		printf '# Files larger than %s bytes. Rewritten by ignore_over50MB.sh.\n' "$max_bytes"
		cat "$list"
		printf '%s\n' "$end"
	} >>"$body"
fi

limit=$(human "$max_bytes")
if [[ ! -f $gitignore ]] && ((count == 0)); then
	echo "No files over ${limit}."
	exit 0
fi
if [[ -f $gitignore ]] && cmp -s "$body" "$gitignore"; then
	if ((seen == 0)); then
		echo "No files over ${limit}."
	else
		echo "No .gitignore changes. ${seen} file(s) over ${limit} already listed."
	fi
	exit 0
fi

mv "$body" "$gitignore"
if ((count == 0)); then
	echo "Updated .gitignore. No files over ${limit}."
else
	echo "Updated .gitignore with ${count} file(s) over ${limit}."
fi
if ((already > 0)); then
	echo "${already} other file(s) over ${limit} were already listed above the managed block."
fi
