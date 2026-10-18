#!/usr/bin/env bash
#
# Bump the Talaia version.
#
# Given a new version X.Y.Z it writes VERSION and moves the content of the
# unreleased section of CHANGELOG.md and CHANGELOG.es.md into a new dated
# release section. It never runs git commit, tag or push.
#
# Usage: scripts/bump-version.sh X.Y.Z
# Set TALAIA_ROOT to operate on a copy of the repository.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [ -n "${TALAIA_ROOT:-}" ]; then
  if [ ! -d "$TALAIA_ROOT" ]; then
    echo "error: TALAIA_ROOT is not a directory: $TALAIA_ROOT" >&2
    exit 1
  fi
  ROOT="$(cd "$TALAIA_ROOT" && pwd)"
else
  ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
fi

if [ "$#" -ne 1 ]; then
  echo "Usage: $(basename "$0") X.Y.Z" >&2
  exit 2
fi

new_version="$1"

if ! printf '%s' "$new_version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "error: invalid version '$new_version' (expected MAJOR.MINOR.PATCH)" >&2
  exit 1
fi

version_file="$ROOT/VERSION"
changelog_en="$ROOT/CHANGELOG.md"
changelog_es="$ROOT/CHANGELOG.es.md"

for file in "$version_file" "$changelog_en" "$changelog_es"; do
  if [ ! -f "$file" ]; then
    echo "error: file not found: $file" >&2
    exit 1
  fi
done

current_version="$(tr -d ' \t\r\n' < "$version_file")"
if [ "$new_version" = "$current_version" ]; then
  echo "error: version is already $new_version" >&2
  exit 1
fi

# Print the number of non-blank lines between the given heading and the next
# level-2 heading.
count_section_lines() {
  local file="$1" heading="$2"
  awk -v heading="$heading" '
    $0 == heading { in_section = 1; next }
    in_section && /^## \[/ { in_section = 0 }
    in_section { line = $0; gsub(/[[:space:]]/, "", line); if (length(line) > 0) count++ }
    END { print count + 0 }
  ' "$file"
}

has_heading() {
  local file="$1" heading="$2"
  awk -v heading="$heading" '
    $0 == heading { found = 1 }
    END { print found + 0 }
  ' "$file"
}

check_changelog() {
  local file="$1" heading="$2"
  if [ "$(has_heading "$file" "$heading")" -eq 0 ]; then
    echo "error: section '$heading' not found in $file" >&2
    exit 1
  fi
  if [ "$(count_section_lines "$file" "$heading")" -eq 0 ]; then
    echo "error: section '$heading' in $file is empty; nothing to release" >&2
    exit 1
  fi
}

check_changelog "$changelog_en" "## [Unreleased]"
check_changelog "$changelog_es" "## [Sin publicar]"

today="$(date +%F)"

# Open a new empty unreleased section above the released one and refresh the
# link references at the bottom of the file.
render_changelog() {
  local file="$1" heading="$2" label="$3" output="$4"
  awk -v newver="$new_version" -v today="$today" -v heading="$heading" -v label="$label" '
    !section_done && $0 == heading {
      print $0
      print ""
      print "## [" newver "] - " today
      section_done = 1
      next
    }
    !links_done && index($0, "[" label "]:") == 1 {
      print "[" label "]: https://github.com/catlinux/Talaia/compare/v" newver "...HEAD"
      print "[" newver "]: https://github.com/catlinux/Talaia/releases/tag/v" newver
      links_done = 1
      next
    }
    { print }
  ' "$file" > "$output"
}

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

render_changelog "$changelog_en" "## [Unreleased]" "Unreleased" "$work_dir/CHANGELOG.md"
render_changelog "$changelog_es" "## [Sin publicar]" "Sin publicar" "$work_dir/CHANGELOG.es.md"
printf '%s\n' "$new_version" > "$work_dir/VERSION"

mv "$work_dir/VERSION" "$version_file"
mv "$work_dir/CHANGELOG.md" "$changelog_en"
mv "$work_dir/CHANGELOG.es.md" "$changelog_es"

echo "Bumped version to $new_version"
