#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# 実環境での検証 (憲章「実環境での検証」/ tasks.md T120, T124) を kind のクラスタで行う。
# 手元と CI (.github/workflows/e2e.yml) は、Makefile の e2e-* ターゲットを経由して本スクリプトを用いる。
#
#   up         kind のクラスタを作り (既にあれば再利用)、cert-manager と本チャートを導入する
#   handshake  集約層を経由しない接続が TLS ハンドシェイクで拒否されることを確かめる (T120)
#   test       実 DPF と検証用ゾーンに対する e2e を直列に実行する
#   down       クラスタを破棄する
#
# kubeconfig は E2E_KUBECONFIG に置き、利用者の kubeconfig と現在のコンテキストを変えない。
# トークンはコマンドラインに置かない (プロセスの一覧に現れるため)。
set -euo pipefail

: "${E2E_KIND_CLUSTER:?}" "${E2E_KUBECONFIG:?}" "${E2E_IMAGE:?}" "${E2E_CERT_MANAGER_VERSION:?}"
: "${E2E_LOCK_OWNER:?}" "${E2E_US2_NAMESPACES:?}"
E2E_TEST_TIMEOUT="${E2E_TEST_TIMEOUT:-80m}"

export KUBECONFIG="$E2E_KUBECONFIG"
node="${E2E_KIND_CLUSTER}-control-plane"
release=dpf-webhook
namespace=cert-manager
selector="app.kubernetes.io/name=cert-manager-iij-dpf-webhook,app.kubernetes.io/instance=${release}"

log() { printf '==> %s\n' "$*" >&2; }

# wait_aggregated は集約層を経由した要求が成功するまで待つ。Pod の入れ替え直後は APIService の
# Available が入れ替え前の値のまま残り、kube-apiserver が新しい Pod へ切り替えるまで 503 を返す。
wait_aggregated() {
	local deadline=$((SECONDS + 120))
	until kubectl get --raw /apis/acme.dns-platform.jp/v1alpha1 >/dev/null 2>&1; do
		if ((SECONDS >= deadline)); then
			kubectl get --raw /apis/acme.dns-platform.jp/v1alpha1 >/dev/null
			return 1
		fi
		sleep 2
	done
}

up() {
	mkdir -p "$(dirname "$E2E_KUBECONFIG")"
	if kind get clusters | grep -qx "$E2E_KIND_CLUSTER"; then
		log "kind のクラスタ ${E2E_KIND_CLUSTER} を再利用する"
		kind export kubeconfig --name "$E2E_KIND_CLUSTER" --kubeconfig "$E2E_KUBECONFIG"
	else
		log "kind のクラスタ ${E2E_KIND_CLUSTER} を作る"
		kind create cluster --name "$E2E_KIND_CLUSTER" --kubeconfig "$E2E_KUBECONFIG" --wait 3m
	fi

	if ! helm status cert-manager -n cert-manager >/dev/null 2>&1; then
		log "cert-manager ${E2E_CERT_MANAGER_VERSION} を導入する"
		helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
			--version "$E2E_CERT_MANAGER_VERSION" --namespace cert-manager --create-namespace \
			--set crds.enabled=true --wait
	fi

	log "イメージ ${E2E_IMAGE} を読み込む"
	kind load docker-image "$E2E_IMAGE" --name "$E2E_KIND_CLUSTER"

	for ns in ${E2E_US2_NAMESPACES//,/ }; do
		kubectl create namespace "$ns" --dry-run=client -o yaml | kubectl apply -f - >/dev/null
	done
	if [[ -n "${DPF_API_TOKEN:-}" ]]; then
		printf '%s' "$DPF_API_TOKEN" | kubectl -n "$namespace" create secret generic dpf-token \
			--from-file=token=/dev/stdin --dry-run=client -o yaml | kubectl apply -f - >/dev/null
	else
		log "DPF_API_TOKEN が無いため、トークンの Secret を作らない"
	fi

	# kind の kube-apiserver はコントロールプレーンのノード上でホストネットワークで動く。
	local apiserver
	apiserver="$(docker inspect "$node" --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')"
	log "本チャートを導入する (lockOwner=${E2E_LOCK_OWNER})"
	helm upgrade --install "$release" ./deploy/chart --namespace "$namespace" \
		--set lockOwner="$E2E_LOCK_OWNER" \
		--set image.repository="${E2E_IMAGE%:*}" --set image.tag="${E2E_IMAGE##*:}" --set image.pullPolicy=Never \
		--set "networkPolicy.kubeAPIServerCIDRs={${apiserver}/32}" \
		--set "tokenSecrets.namespaces={${E2E_US2_NAMESPACES}}" \
		--wait --timeout 5m
	# 同じタグで作り直したイメージを用いるため、再利用したクラスタでも Pod を入れ替える。
	kubectl -n "$namespace" rollout restart "deploy/${release}" >/dev/null
	kubectl -n "$namespace" rollout status "deploy/${release}" --timeout 5m
	kubectl wait apiservice/v1alpha1.acme.dns-platform.jp --for=condition=Available --timeout=5m
	wait_aggregated
}

handshake() {
	log "集約層を経由した要求が受理されることを確かめる"
	wait_aggregated

	# コントロールプレーンのノードから Pod へ直接つなぐ。port-forward は、サーバが接続を
	# 切った際の RST で終了するため用いない。
	local bin pod_ip
	bin="$(mktemp -d)/e2e.test"
	CGO_ENABLED=0 go test -c -tags e2e -o "$bin" ./test/e2e/
	docker cp "$bin" "${node}:/e2e.test"
	rm -rf "$(dirname "$bin")"
	pod_ip="$(kubectl -n "$namespace" get pods -l "$selector" \
		--field-selector=status.phase=Running -o jsonpath='{.items[0].status.podIP}')"
	log "Pod (${pod_ip}) へ直接つなぐ接続が拒否されることを確かめる"
	docker exec -e E2E_WEBHOOK_ADDR="${pod_ip}:10443" "$node" \
		/e2e.test -test.v -test.count=1 -test.run '^TestWebhookRequiresClientCertificate$'
}

run_tests() {
	if [[ -z "${DPF_API_TOKEN:-}" || -z "${DPF_TEST_ZONE:-}" ]]; then
		echo "DPF_API_TOKEN と DPF_TEST_ZONE が必要" >&2
		exit 1
	fi
	log "実環境の検証を直列に実行する (検証用ゾーン ${DPF_TEST_ZONE})"
	E2E_CLUSTER=1 E2E_US2_NAMESPACES="$E2E_US2_NAMESPACES" \
		go test -tags e2e -p 1 -count=1 -timeout "$E2E_TEST_TIMEOUT" -v ${E2E_RUN:+-run "$E2E_RUN"} ./test/e2e/
}

down() {
	kind delete cluster --name "$E2E_KIND_CLUSTER"
	rm -f "$E2E_KUBECONFIG"
}

case "${1:-}" in
up) up ;;
handshake) handshake ;;
test) run_tests ;;
down) down ;;
*)
	echo "usage: $0 up|handshake|test|down" >&2
	exit 2
	;;
esac
