#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
checkout_dir="$(mktemp -d)"
trap 'rm -rf "${checkout_dir}"' EXIT

while IFS= read -r patch_path; do
  relative_path="${patch_path#"${repo_root}/"}"
  mkdir -p "${checkout_dir}/$(dirname "${relative_path}")"
  git -C "${repo_root}" -c core.autocrlf=true checkout-index \
    --prefix="${checkout_dir}/" -- "${relative_path}"
  git -C "${repo_root}" cat-file blob ":${relative_path}" \
    | cmp - "${checkout_dir}/${relative_path}"
done < <(find "${repo_root}/build/patches" -type f -name '*.patch' -print | sort)
