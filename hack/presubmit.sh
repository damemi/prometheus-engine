#!/usr/bin/env bash

# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     https:#www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

source .bingo/variables.env

usage() {
      cat >&2 << EOF
usage: $(basename "$0") [all] [codegen] [crdgen] [diff] [docgen] [manifests] [format] [test]
  $(basename "$0") executes presubmit tasks on the respository to prepare code
  before submitting changes. Running with no arguments runs every check
  (i.e. the 'all' subcommand).

EOF
}

warn() {
  echo "[$(date +'%Y-%m-%dT%H:%M:%S%z')]: Warning: $*" >&2
}

REPO_ROOT=$(dirname $(dirname "${BASH_SOURCE[0]}"))
CRD_DIR=${REPO_ROOT}/charts/operator/crds

codegen_diff() {
  git fetch https://github.com/GoogleCloudPlatform/prometheus-engine
  git merge-base --is-ancestor FETCH_HEAD HEAD || warn "Current commit is not a descendent of main branch. Consider rebasing."
  APIS_DIR=$(git rev-parse --show-toplevel)/pkg/operator/apis
  git diff -s --exit-code FETCH_HEAD -- "${APIS_DIR}"
}

update_codegen() {
  echo ">>> regenerating CRD k8s go code"

  # Refresh vendored dependencies to ensure script is found.
  go mod vendor

  CODEGEN_PKG=${CODEGEN_PKG:-$(cd "${REPO_ROOT}"; ls -d -1 ./vendor/k8s.io/code-generator 2>/dev/null || echo ../code-generator)}
}

combine() {
  SOURCE_DIR=$1
  DEST_YAML=$2

  mkdir -p $(dirname ${DEST_YAML})
  cat "${REPO_ROOT}/hack/boilerplate.txt" > "$DEST_YAML"
  printf "\n# NOTE: This file is autogenerated.\n" >> $DEST_YAML
  ${YQ} '... comments=""' ${SOURCE_DIR}/*.yaml >> "$DEST_YAML"
}

update_crdgen() {
  echo ">>> regenerating CRD yamls"

  # TODO(TheSpiritXIII): Replace once merged: https://github.com/kubernetes-sigs/controller-tools/pull/878
  which controller-gen || go install github.com/TheSpiritXIII/controller-tools/cmd/controller-gen@v0.14.1-gmp

  API_DIR=${REPO_ROOT}/pkg/operator/apis/...
  controller-gen crd paths=./${API_DIR} output:crd:dir=${CRD_DIR}

  CRD_YAMLS=$(find ${CRD_DIR} -iname '*.yaml' | sort)
  for i in $CRD_YAMLS; do
    sed -i '0,/---/{/---/d}' $i
    # removed the crd status hack , see https://github.com/kubernetes-sigs/controller-tools/pull/630
    echo "$(cat $i)" > $i
    echo -e "$(cat ${REPO_ROOT}/hack/boilerplate.txt)\n$(cat $i)" > $i
  done

  combine ${CRD_DIR} ${REPO_ROOT}/manifests/setup.yaml
}

update_docgen() {
  echo ">>> generating API documentation"
  mkdir -p doc
  gen-crd-api-reference-docs \
    -config "./hack/gen-crd/config.json" \
    -template-dir "./hack/gen-crd" \
    -api-dir "./pkg/operator/apis/monitoring/v1" \
    -out-file "./doc/api.md"
}

update_manifests() {
  echo ">>> regenerating example yamls"

  combine "${CRD_DIR}" "${REPO_ROOT}/manifests/setup.yaml"
  ${HELM} template "${REPO_ROOT}/charts/operator" \
    -f "${REPO_ROOT}/charts/values.global.yaml" \
     > "${REPO_ROOT}/manifests/operator.yaml"
  ${HELM} template "${REPO_ROOT}/charts/operator" \
    -f "${REPO_ROOT}/charts/values.global.yaml" \
    -f "${REPO_ROOT}/charts/max-throughput.yaml" \
    | ${YQ} e '. | select(.kind == "DaemonSet")' \
     > "${REPO_ROOT}/examples/collector-max-throughput.yaml"
  ${HELM} template "${REPO_ROOT}/charts/rule-evaluator" \
   -f "${REPO_ROOT}/charts/values.global.yaml" \
    > "${REPO_ROOT}/manifests/rule-evaluator.yaml"
  # TODO(bwplotka): Unify output paths (has to be synced with GCP docs).
  ${HELM} template "${REPO_ROOT}/charts/datasource-syncer" \
     -f "${REPO_ROOT}/charts/values.global.yaml" \
      > "${REPO_ROOT}/cmd/datasource-syncer/datasource-syncer.yaml"

  echo "REPO_ROOT=${REPO_ROOT}"
  echo "PWD=${PWD}"
  ls manifests examples

  ${ADDLICENSE} "${REPO_ROOT}"/manifests/*.yaml "${REPO_ROOT}"/examples/*.yaml "${REPO_ROOT}"/cmd/datasource-syncer/datasource-syncer.yaml
}

run_tests() {
  echo ">>> running unit tests"
  go test `go list ${REPO_ROOT}/... | grep -v e2e | grep -v export/bench | grep -v export/gcm`
}

reformat() {
  go mod tidy && go mod vendor && go fmt ${REPO_ROOT}/...
  ${MDOX} fmt --soft-wraps ${REPO_ROOT}/*.md ${REPO_ROOT}/cmd/**/*.md
}

exit_msg() {
  echo $1
  exit 1
}

update_all() {
  # As this command can be slow, optimize by only running if there's difference
  # from the origin/main branch.
  codegen_diff || update_codegen
  reformat
  update_crdgen
  update_manifests
  update_docgen
}

main() {
  if [[ -z "$@" ]]; then
    update_all
  else
    for opt in "$@"; do
      case "${opt}" in
        all)
          update_all
          ;;
        codegen)
          update_codegen
          ;;
        crdgen)
          update_crdgen
          ;;
        diff)
          git diff --exit-code doc go.mod go.sum '*.go' '*.yaml' || \
            exit_msg "diff found - ensure regenerated code is up-to-date and committed."
          ;;
        docgen)
          update_docgen
          ;;
        manifests)
          update_manifests
          ;;
        format)
          reformat
          ;;
        test)
          run_tests
          ;;
        *)
          printf "unsupported command: \"${opt}\".\n"
          usage
      esac
    done
  fi
}

main "$@"
