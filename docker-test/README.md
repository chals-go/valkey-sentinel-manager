# Docker Test Environment

[English](#english) | [한국어](#한국어)

---

# English

A full Docker Compose test environment for Valkey Sentinel Manager. One script brings up 16 containers — Valkey replication groups, Sentinel clusters, Mock DNS API, and Sentinel Manager — ready for end-to-end testing.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    Docker Compose Network                       │
│                                                                 │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐        │
│  │ valkey7       │   │ valkey8       │   │ valkey8x      │       │
│  │ primary       │   │ primary       │   │ primary       │       │
│  │ + replica x2  │   │ + replica x1  │   │ + replica x1  │       │
│  │ (v7.2)        │   │ (v8.0)        │   │ (v8.1)        │       │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘        │
│         │                  │                   │                │
│  ┌──────┴───────┐   ┌──────┴───────────────────┴───────┐       │
│  │ Sentinel      │   │ Sentinel Cluster B               │       │
│  │ Cluster A     │   │ (sentinel-b1/b2/b3)              │       │
│  │ (a1/a2/a3)    │   │ + sentinel-agent                 │       │
│  │ + sentinel-   │   │ monitors: mymaster-v8,            │       │
│  │   agent       │   │           mymaster-v8x            │       │
│  │ monitors:     │   └──────────────┬──────────────────┘       │
│  │ mymaster-v7   │                  │                           │
│  └──────┬───────┘                  │                           │
│         │          ┌───────────────┘                           │
│         │          │                                            │
│  ┌──────┴──────────┴──────┐  ┌──────────┐  ┌──────────────┐   │
│  │ Sentinel Manager       │  │ Mock DNS  │  │ Store Valkey │   │
│  │ :8000                  ├──┤ REST API  │  │ (data store) │   │
│  │ (all DNS providers)    │  │ :8080     │  │              │   │
│  └────────────────────────┘  └──────────┘  └──────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Containers (16)

| Container | Image | Role |
|-----------|-------|------|
| **store-valkey** | valkey 8.0 | Sentinel Manager data store |
| **valkey7-primary** | valkey 7.2 | Replication group primary |
| **valkey7-replica1** | valkey 7.2 | Replication group replica |
| **valkey7-replica2** | valkey 7.2 | Replication group replica |
| **valkey8-primary** | valkey 8.0 | Replication group primary |
| **valkey8-replica1** | valkey 8.0 | Replication group replica |
| **valkey8x-primary** | valkey 8.1 | Replication group primary |
| **valkey8x-replica1** | valkey 8.1 | Replication group replica |
| **sentinel-a1/a2/a3** | valkey 7.2 + agent | Sentinel Cluster A (monitors mymaster-v7) |
| **sentinel-b1/b2/b3** | valkey 8.0 + agent | Sentinel Cluster B (monitors mymaster-v8, mymaster-v8x) |
| **mock-dns** | Go HTTP server | Mock DNS REST API (accepts all requests, logs history) |
| **sentinel-manager** | Go binary | Sentinel Manager with all DNS providers |

## Prerequisites

- Docker
- Docker Compose v2+

## Quick Start

```bash
cd docker-test
bash start.sh
```

The script builds images, starts containers in dependency order, pre-configures API token and DNS provider, then prints connection info.

## Access

| Item | Value |
|------|-------|
| **Web UI** | http://localhost:8000/admin/ |
| **Login** | `admin` / `admin` |
| **API Token** | `smgr_docker_test_token_2026` |

## Pre-configured

The setup container automatically configures:

- **API Token** — `smgr_docker_test_token_2026` (stored in Valkey + shared to all sentinel agents)
- **DNS Provider** — `mock-dns` (REST API type, pointing to http://mock-dns:8080)

## Manual Registration

After starting, register these in the Web UI:

### Sentinel Clusters

Go to **Sentinel Cluster** → **Register**

| Group Name | Nodes | Monitors |
|------------|-------|----------|
| cluster-a | sentinel-a1:26379, sentinel-a2:26379, sentinel-a3:26379 | mymaster-v7 |
| cluster-b | sentinel-b1:26379, sentinel-b2:26379, sentinel-b3:26379 | mymaster-v8, mymaster-v8x |

### Replication Groups

Go to **Replication Group** → **Register** (or use **Load Sentinels** for auto-import)

| Monitoring Name | Sentinel Cluster | Primary | Version | Replicas |
|-----------------|------------------|---------|---------|----------|
| mymaster-v7 | cluster-a | valkey7-primary:6379 | 7.2 | 2 replicas |
| mymaster-v8 | cluster-b | valkey8-primary:6379 | 8.0 | 1 replica |
| mymaster-v8x | cluster-b | valkey8x-primary:6379 | 8.1 | 1 replica |

> **Tip**: Use the **Load Sentinels** button to auto-import all masters from a sentinel cluster.
> Select `mock-dns` as the DNS provider when registering replication groups.

## Failover Test

To trigger a manual failover, stop a primary container:

```bash
# Stop valkey7 primary to trigger failover
docker stop valkey7-primary

# Watch sentinel logs
docker compose logs -f sentinel-a1

# Check mock-dns received DNS update requests
docker exec mock-dns wget -qO- http://localhost:8080/history
```

## Commands

```bash
docker compose ps          # Container status
docker compose logs -f     # Follow all logs
docker compose down -v     # Tear down everything (including volumes)
```

## File Structure

```
docker-test/
├── start.sh               # One-click start script
├── docker-compose.yml      # 16 service definitions
├── manager-config.yaml     # Sentinel Manager config
├── setup.sh                # Pre-configures API token + DNS provider
├── mock-dns/
│   ├── Dockerfile          # Mock DNS API image
│   └── main.go             # Simple HTTP server (logs all requests)
├── sentinel/
│   ├── Dockerfile          # Sentinel + agent image
│   └── entrypoint.sh       # Generates sentinel.conf + agent config
└── README.md               # This file
```

---

# 한국어

Valkey Sentinel Manager를 위한 Docker Compose 테스트 환경입니다. 스크립트 하나로 16개 컨테이너 — Valkey Replication Group, Sentinel 클러스터, Mock DNS API, Sentinel Manager — 를 띄워 전체 기능을 테스트할 수 있습니다.

## 아키텍처

```
┌─────────────────────────────────────────────────────────────────┐
│                    Docker Compose Network                       │
│                                                                 │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────┐        │
│  │ valkey7       │   │ valkey8       │   │ valkey8x      │       │
│  │ primary       │   │ primary       │   │ primary       │       │
│  │ + replica x2  │   │ + replica x1  │   │ + replica x1  │       │
│  │ (v7.2)        │   │ (v8.0)        │   │ (v8.1)        │       │
│  └──────┬───────┘   └──────┬───────┘   └──────┬───────┘        │
│         │                  │                   │                │
│  ┌──────┴───────┐   ┌──────┴───────────────────┴───────┐       │
│  │ Sentinel      │   │ Sentinel Cluster B               │       │
│  │ Cluster A     │   │ (sentinel-b1/b2/b3)              │       │
│  │ (a1/a2/a3)    │   │ + sentinel-agent                 │       │
│  │ + sentinel-   │   │ 모니터링: mymaster-v8,             │       │
│  │   agent       │   │          mymaster-v8x             │       │
│  │ 모니터링:      │   └──────────────┬──────────────────┘       │
│  │ mymaster-v7   │                  │                           │
│  └──────┬───────┘                  │                           │
│         │          ┌───────────────┘                           │
│         │          │                                            │
│  ┌──────┴──────────┴──────┐  ┌──────────┐  ┌──────────────┐   │
│  │ Sentinel Manager       │  │ Mock DNS  │  │ Store Valkey │   │
│  │ :8000                  ├──┤ REST API  │  │ (데이터 저장소)│   │
│  │ (전체 DNS provider)     │  │ :8080     │  │              │   │
│  └────────────────────────┘  └──────────┘  └──────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## 컨테이너 (16개)

| 컨테이너 | 이미지 | 역할 |
|----------|--------|------|
| **store-valkey** | valkey 8.0 | Sentinel Manager 데이터 저장소 |
| **valkey7-primary** | valkey 7.2 | Replication Group primary |
| **valkey7-replica1/2** | valkey 7.2 | Replication Group replica |
| **valkey8-primary** | valkey 8.0 | Replication Group primary |
| **valkey8-replica1** | valkey 8.0 | Replication Group replica |
| **valkey8x-primary** | valkey 8.1 | Replication Group primary |
| **valkey8x-replica1** | valkey 8.1 | Replication Group replica |
| **sentinel-a1/a2/a3** | valkey 7.2 + agent | Sentinel Cluster A (mymaster-v7 모니터링) |
| **sentinel-b1/b2/b3** | valkey 8.0 + agent | Sentinel Cluster B (mymaster-v8, mymaster-v8x 모니터링) |
| **mock-dns** | Go HTTP 서버 | Mock DNS REST API (모든 요청 수락, 히스토리 기록) |
| **sentinel-manager** | Go 바이너리 | 전체 DNS provider 포함 빌드 |

## 사전 요구사항

- Docker
- Docker Compose v2+

## 빠른 시작

```bash
cd docker-test
bash start.sh
```

이미지 빌드 → 의존성 순서대로 컨테이너 시작 → API 토큰/DNS provider 자동 설정 → 접속 정보 출력까지 한 번에 수행됩니다.

## 접속 정보

| 항목 | 값 |
|------|---|
| **Web UI** | http://localhost:8000/admin/ |
| **로그인** | `admin` / `admin` |
| **API Token** | `smgr_docker_test_token_2026` |

## 자동 설정 항목

setup 컨테이너가 자동으로 설정하는 항목:

- **API Token** — `smgr_docker_test_token_2026` (Valkey에 저장 + 모든 sentinel agent에 공유)
- **DNS Provider** — `mock-dns` (REST API 타입, http://mock-dns:8080)

## 수동 등록 필요 항목

시작 후 Web UI에서 등록해야 하는 항목:

### Sentinel Cluster 등록

**Sentinel Cluster** → **등록**

| 그룹 이름 | 노드 | 모니터링 대상 |
|-----------|------|-------------|
| cluster-a | sentinel-a1:26379, sentinel-a2:26379, sentinel-a3:26379 | mymaster-v7 |
| cluster-b | sentinel-b1:26379, sentinel-b2:26379, sentinel-b3:26379 | mymaster-v8, mymaster-v8x |

### Replication Group 등록

**Replication Group** → **등록** (또는 **Load Sentinels**로 일괄 등록)

| Monitoring Name | Sentinel Cluster | Primary | 버전 | Replica |
|-----------------|------------------|---------|------|---------|
| mymaster-v7 | cluster-a | valkey7-primary:6379 | 7.2 | 2개 |
| mymaster-v8 | cluster-b | valkey8-primary:6379 | 8.0 | 1개 |
| mymaster-v8x | cluster-b | valkey8x-primary:6379 | 8.1 | 1개 |

> **Tip**: **Load Sentinels** 버튼으로 sentinel cluster에서 모니터링 중인 마스터를 자동 등록할 수 있습니다.
> Replication Group 등록 시 DNS provider로 `mock-dns`를 선택하세요.

## 페일오버 테스트

Primary 컨테이너를 중지하면 페일오버가 발생합니다:

```bash
# valkey7 primary를 중지하여 페일오버 트리거
docker stop valkey7-primary

# sentinel 로그 확인
docker compose logs -f sentinel-a1

# mock-dns가 수신한 DNS 업데이트 요청 확인
docker exec mock-dns wget -qO- http://localhost:8080/history
```

## 관리 명령어

```bash
docker compose ps          # 컨테이너 상태
docker compose logs -f     # 전체 로그
docker compose down -v     # 종료 (볼륨 포함 삭제)
```

## 파일 구조

```
docker-test/
├── start.sh               # 원클릭 실행 스크립트
├── docker-compose.yml      # 16개 서비스 정의
├── manager-config.yaml     # Sentinel Manager 설정
├── setup.sh                # API 토큰 + DNS provider 자동 설정
├── mock-dns/
│   ├── Dockerfile          # Mock DNS API 이미지
│   └── main.go             # 간단한 HTTP 서버 (모든 요청 로깅)
├── sentinel/
│   ├── Dockerfile          # Sentinel + agent 이미지
│   └── entrypoint.sh       # sentinel.conf + agent 설정 생성
└── README.md               # 이 파일
```
