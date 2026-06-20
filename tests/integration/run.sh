#!/bin/sh
# Integration test driver for the Namecheap library.
#
# Usage: run.sh live
#
# Talks to the real Namecheap API, so it needs credentials and a disposable test
# domain in the environment:
#   NAMECHEAP_USER_NAME, NAMECHEAP_API_USER, NAMECHEAP_API_KEY   API credentials
#   NAMECHEAP_TEST_DOMAIN    a domain on the account, managed and torn down
#   NAMECHEAP_CLIENT_IP      optional whitelisted client IP (default 0.0.0.0)
#   NAMECHEAP_USE_SANDBOX    optional, 'true' to use the sandbox API
#   UNOBIN_VERSION           version of unobin to build the stack with
#   SCENARIO                 optional, run only one scenario directory
#
# Each scenario is created, verified, updated, then destroyed. Destroy runs even
# after an earlier failure, so a run does not leave records or a custom
# delegation behind on the test domain.

set -eu

if [ "${1:-}" != "live" ]; then
    echo "usage: ${0} live" >&2
    exit 2
fi

: "${NAMECHEAP_USER_NAME:?NAMECHEAP_USER_NAME is required}"
: "${NAMECHEAP_API_USER:?NAMECHEAP_API_USER is required}"
: "${NAMECHEAP_API_KEY:?NAMECHEAP_API_KEY is required}"
: "${NAMECHEAP_TEST_DOMAIN:?NAMECHEAP_TEST_DOMAIN is required}"
UNOBIN_VERSION="${UNOBIN_VERSION:?UNOBIN_VERSION is required}"

# The scenarios declare the domain as a required input; the account-specific
# value reaches every plan through this override rather than the committed
# config files.
export UB_VAR_domain="${NAMECHEAP_TEST_DOMAIN}"

SCRIPT_DIR="$(cd "$(dirname "${0}")" && pwd)"
REPO_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SCENARIOS_DIR="${SCRIPT_DIR}/scenarios"

# Populate the scenario list. Iterate every directory under scenarios/ unless
# SCENARIO names one.
SELECT="${SCENARIO:-}"
if [ -n "${SELECT}" ]; then
    if [ ! -d "${SCENARIOS_DIR}/${SELECT}" ]; then
        echo "SCENARIO=${SELECT} not found under ${SCENARIOS_DIR}" >&2
        exit 2
    fi
    set -- "${SCENARIOS_DIR}/${SELECT}"
else
    set --
    for d in "${SCENARIOS_DIR}"/*/; do
        [ -d "${d}" ] || continue
        set -- "${@}" "${d%/}"
    done
fi
if [ ${#} -eq 0 ]; then
    echo "no scenarios under ${SCENARIOS_DIR}" >&2
    exit 2
fi

go install github.com/cloudboss/unobin/cmd/unobin@${UNOBIN_VERSION}
UNOBIN="${GOPATH:-${HOME}/go}/bin/unobin"

mkdir -p "${REPO_DIR}/_output/integration"
tmp_dir=$(mktemp -d "${REPO_DIR}/_output/integration/run-XXXXXX")

FAILED=""
COUNT=0
for sdir in "${@}"; do
    COUNT=$((COUNT + 1))
    name=$(basename "${sdir}")
    echo "==> live/${name}"

    missing=""
    for f in factory.ub config.ub config-update.ub; do
        [ -f "${sdir}/${f}" ] || { echo "missing ${f}" >&2; missing="true"; }
    done
    if [ -n "${missing}" ]; then
        FAILED="${FAILED} ${name}(missing-config)"
        continue
    fi

    build_dir="${tmp_dir}/${name}"
    rel="${sdir#${REPO_DIR}/}"

    # failed_step holds the first failure and stays the reported reason. Once
    # apply runs, the scenario has changed the test domain, so destroy runs even
    # after a later failure to put it back. destroy_config names the config of
    # the most recent apply, so the destroy plans from the same inputs.
    failed_step=""
    applied=""
    destroy_config="config.ub"

    ${UNOBIN} compile -p "${sdir}/factory.ub" -o "${build_dir}" --build || failed_step="compile"

    # The update config keeps its basename under update/ so both passes plan
    # under the same stack name. The domain reaches each plan through the
    # UB_VAR_domain override, so the configs are staged as written.
    if [ -z "${failed_step}" ]; then
        (
            cd "${build_dir}"
            mkdir -p update
            cp "${sdir}/config.ub" config.ub
            cp "${sdir}/config-update.ub" update/config.ub
            ./${name} pin -c config.ub
            ./${name} pin -c update/config.ub
        ) || failed_step="pin"
    fi

    if [ -z "${failed_step}" ]; then
        ( cd "${build_dir}" && ./${name} plan -c ./config.ub -o plan.json ) || failed_step="plan"
    fi

    if [ -z "${failed_step}" ]; then
        applied="true"
        ( cd "${build_dir}" && ./${name} apply plan.json ) || failed_step="apply"
    fi

    if [ -z "${failed_step}" ]; then
        ( cd "${REPO_DIR}" && VERIFY_PHASE=applied go run "./${rel}/verify" ) \
            || failed_step="verify-applied"
    fi

    if [ -z "${failed_step}" ]; then
        ( cd "${build_dir}" && ./${name} plan -c ./update/config.ub -o plan-update.json ) \
            || failed_step="plan-update"
    fi

    if [ -z "${failed_step}" ]; then
        destroy_config="update/config.ub"
        ( cd "${build_dir}" && ./${name} apply plan-update.json ) || failed_step="apply-update"
    fi

    # Destroy runs whenever apply was attempted, even after a failure, so a run
    # still cleans up the test domain. verify-destroyed runs only on an otherwise
    # clean run; a destroy failure is appended, since it means the domain was
    # left changed.
    if [ -n "${applied}" ]; then
        if (
            cd "${build_dir}"
            ./${name} plan --destroy -c "./${destroy_config}" -o destroy.json
            ./${name} apply destroy.json
        ); then
            if [ -z "${failed_step}" ]; then
                ( cd "${REPO_DIR}" && VERIFY_PHASE=destroyed go run "./${rel}/verify" ) \
                    || failed_step="verify-destroyed"
            fi
        elif [ -z "${failed_step}" ]; then
            failed_step="destroy"
        else
            failed_step="${failed_step}+destroy"
        fi
    fi

    [ -n "${failed_step}" ] && FAILED="${FAILED} ${name}(${failed_step})"
done

if [ -n "${FAILED}" ]; then
    echo "FAIL:${FAILED}" >&2
    exit 1
fi
echo "OK: ${COUNT} scenario(s)"
