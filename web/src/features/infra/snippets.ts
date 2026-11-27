export type SnippetGroup = "queues" | "databases" | "search" | "storage" | "observability" | "tools";

export interface Snippet {
  id: string;
  label: string;
  group: SnippetGroup;
  requires?: string[];
  body: (prefix: string) => string;
}

export const snippetGroups: SnippetGroup[] = ["queues", "databases", "search", "storage", "observability", "tools"];

export function detectPrefix(compose: string): string {
  const m = /container_name:\s*["']?([^\s"'$]+)_[a-z0-9-]+["']?\s*$/m.exec(compose);
  return m?.[1] ?? "dev";
}

function head(p: string, name: string, image: string, expose: number[]): string {
  return `  ${name}:
    image: ${image}
    container_name: ${p}_${name}
    hostname: ${name}
    networks:
      - sudoku_network
    expose:
${expose.map((e) => `      - "${e}/tcp"`).join("\n")}
    restart: unless-stopped
`;
}

function web(router: string, host: string, port: number): string {
  return `      - "traefik.http.routers.${router}.entrypoints=web_80"
      - "traefik.http.routers.${router}.rule=Host(\`${host}.localhost\`)"
      - "traefik.http.routers.${router}.service=${router}"
      - "traefik.http.services.${router}.loadBalancer.server.port=${port}"
`;
}

function labels(...routes: string[]): string {
  return `    labels:
      - "traefik.enable=true"
${routes.join("")}`;
}

function data(name: string, target: string): string {
  return `    volumes:
      - ./sudoku-config/${name}/data:${target}:delegated
`;
}

export const snippets: Snippet[] = [
  {
    id: "rabbitmq",
    label: "RabbitMQ",
    group: "queues",
    body: (p) =>
      head(p, "rabbitmq", "rabbitmq:3.13-management", [5672, 15672]) +
      data("rabbitmq", "/var/lib/rabbitmq") +
      `    environment:
      RABBITMQ_DEFAULT_USER: rabbitmq
      RABBITMQ_DEFAULT_PASS: rabbitmq
` +
      labels(
        `      - "traefik.tcp.routers.rabbitmq.entrypoints=rabbitmq"
      - "traefik.tcp.routers.rabbitmq.rule=HostSNI(\`*\`)"
      - "traefik.tcp.routers.rabbitmq.service=rabbitmq"
      - "traefik.tcp.services.rabbitmq.loadBalancer.server.port=5672"
`,
        web("rabbitmq-mgmt", "rabbitmq", 15672),
      ),
  },
  {
    id: "nats",
    label: "NATS",
    group: "queues",
    body: (p) =>
      head(p, "nats", "nats:2.10-alpine", [4222, 8222]) +
      `    command: ["--jetstream", "--store_dir", "/data", "--http_port", "8222"]
` +
      data("nats", "/data") +
      labels(web("nats", "nats", 8222)),
  },
  {
    id: "kafka",
    label: "Kafka",
    group: "queues",
    body: (p) =>
      head(p, "kafka", "apache/kafka:3.9.0", [9092]) +
      data("kafka", "/var/lib/kafka/data") +
      `    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_LISTENERS: PLAINTEXT://:9092,CONTROLLER://:9093
      KAFKA_ADVERTISED_LISTENERS: PLAINTEXT://kafka:9092
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_LISTENER_SECURITY_PROTOCOL_MAP: CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT
      KAFKA_CONTROLLER_QUORUM_VOTERS: 1@kafka:9093
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR: 1
      KAFKA_TRANSACTION_STATE_LOG_MIN_ISR: 1
      KAFKA_GROUP_INITIAL_REBALANCE_DELAY_MS: 0
      KAFKA_NUM_PARTITIONS: 3
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "true"
      KAFKA_LOG_DIRS: /var/lib/kafka/data
`,
  },
  {
    id: "kafka-ui",
    label: "Kafka UI",
    group: "queues",
    requires: ["kafka"],
    body: (p) =>
      head(p, "kafka-ui", "provectuslabs/kafka-ui:v0.7.2", [8080]) +
      `    depends_on:
      - kafka
    environment:
      KAFKA_CLUSTERS_0_NAME: local
      KAFKA_CLUSTERS_0_BOOTSTRAPSERVERS: kafka:9092
      DYNAMIC_CONFIG_ENABLED: "true"
` +
      labels(web("kafka-ui", "kafka-ui", 8080)),
  },
  {
    id: "centrifugo",
    label: "Centrifugo",
    group: "queues",
    body: (p) =>
      head(p, "centrifugo", "centrifugo/centrifugo:v5", [8000]) +
      `    command: centrifugo -c config.json
    volumes:
      - ./sudoku-config/centrifugo/config.json:/centrifugo/config.json:ro
    ulimits:
      nofile:
        soft: 65535
        hard: 65535
` +
      labels(web("centrifugo", "centrifugo", 8000)),
  },
  {
    id: "postgres",
    label: "PostgreSQL",
    group: "databases",
    body: (p) =>
      head(p, "postgres", "postgres:17-alpine", [5432]) +
      data("postgres", "/var/lib/postgresql/data") +
      `    environment:
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 5s
      retries: 10
`,
  },
  {
    id: "mysql",
    label: "MySQL",
    group: "databases",
    body: (p) =>
      head(p, "mysql", "mysql:8.4", [3306]) +
      data("mysql", "/var/lib/mysql") +
      `    environment:
      MYSQL_ROOT_PASSWORD: root
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-proot"]
      interval: 5s
      retries: 10
`,
  },
  {
    id: "mariadb",
    label: "MariaDB",
    group: "databases",
    body: (p) =>
      head(p, "mariadb", "mariadb:11.4", [3306]) +
      data("mariadb", "/var/lib/mysql") +
      `    environment:
      MARIADB_ROOT_PASSWORD: root
    healthcheck:
      test: ["CMD", "healthcheck.sh", "--connect", "--innodb_initialized"]
      interval: 5s
      retries: 10
`,
  },
  {
    id: "mongo",
    label: "MongoDB",
    group: "databases",
    body: (p) =>
      head(p, "mongo", "mongo:8", [27017]) +
      data("mongo", "/data/db") +
      `    environment:
      MONGO_INITDB_ROOT_USERNAME: root
      MONGO_INITDB_ROOT_PASSWORD: root
`,
  },
  {
    id: "clickhouse",
    label: "ClickHouse",
    group: "databases",
    body: (p) =>
      head(p, "clickhouse", "clickhouse/clickhouse-server:24.8", [8123, 9000]) +
      data("clickhouse", "/var/lib/clickhouse") +
      `    environment:
      CLICKHOUSE_USER: clickhouse
      CLICKHOUSE_PASSWORD: clickhouse
      CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT: 1
    ulimits:
      nofile:
        soft: 262144
        hard: 262144
` +
      labels(web("clickhouse", "clickhouse", 8123)),
  },
  {
    id: "valkey",
    label: "Valkey",
    group: "databases",
    body: (p) => head(p, "valkey", "valkey/valkey:8-alpine", [6379]) + data("valkey", "/data"),
  },
  {
    id: "memcached",
    label: "Memcached",
    group: "databases",
    body: (p) => head(p, "memcached", "memcached:1.6-alpine", [11211]) + `    command: ["memcached", "-m", "256"]\n`,
  },
  {
    id: "meilisearch",
    label: "Meilisearch",
    group: "search",
    body: (p) =>
      head(p, "meilisearch", "getmeili/meilisearch:v1.11", [7700]) +
      data("meilisearch", "/meili_data") +
      `    environment:
      MEILI_ENV: development
      MEILI_MASTER_KEY: masterKey
` +
      labels(web("meilisearch", "meilisearch", 7700)),
  },
  {
    id: "opensearch",
    label: "OpenSearch",
    group: "search",
    body: (p) =>
      head(p, "opensearch", "opensearchproject/opensearch:2.17.1", [9200]) +
      data("opensearch", "/usr/share/opensearch/data") +
      `    environment:
      discovery.type: single-node
      DISABLE_SECURITY_PLUGIN: "true"
      DISABLE_INSTALL_DEMO_CONFIG: "true"
      OPENSEARCH_JAVA_OPTS: -Xms512m -Xmx512m
` +
      labels(web("opensearch", "opensearch", 9200)),
  },
  {
    id: "typesense",
    label: "Typesense",
    group: "search",
    body: (p) =>
      head(p, "typesense", "typesense/typesense:27.1", [8108]) +
      `    command: ["--data-dir", "/data", "--api-key", "typesense", "--enable-cors"]
` +
      data("typesense", "/data") +
      labels(web("typesense", "typesense", 8108)),
  },
  {
    id: "qdrant",
    label: "Qdrant",
    group: "search",
    body: (p) => head(p, "qdrant", "qdrant/qdrant:v1.12.4", [6333, 6334]) + data("qdrant", "/qdrant/storage") + labels(web("qdrant", "qdrant", 6333)),
  },
  {
    id: "seaweedfs",
    label: "SeaweedFS (S3)",
    group: "storage",
    body: (p) =>
      head(p, "seaweedfs", "chrislusf/seaweedfs:3.97", [8333, 9333, 8888]) +
      `    command: ["server", "-dir=/data", "-s3", "-s3.port=8333"]
` +
      data("seaweedfs", "/data") +
      labels(web("seaweedfs-s3", "s3", 8333), web("seaweedfs", "seaweedfs", 9333)),
  },
  {
    id: "localstack",
    label: "LocalStack (AWS)",
    group: "storage",
    body: (p) => head(p, "localstack", "localstack/localstack:3.8", [4566]) + data("localstack", "/var/lib/localstack") + labels(web("localstack", "localstack", 4566)),
  },
  {
    id: "jaeger",
    label: "Jaeger",
    group: "observability",
    body: (p) =>
      head(p, "jaeger", "jaegertracing/all-in-one:1.62.0", [16686, 4317, 4318]) +
      `    environment:
      COLLECTOR_OTLP_ENABLED: "true"
` +
      labels(web("jaeger", "jaeger", 16686)),
  },
  {
    id: "prometheus",
    label: "Prometheus",
    group: "observability",
    body: (p) => head(p, "prometheus", "prom/prometheus:v2.55.1", [9090]) + labels(web("prometheus", "prometheus", 9090)),
  },
  {
    id: "grafana",
    label: "Grafana",
    group: "observability",
    body: (p) =>
      head(p, "grafana", "grafana/grafana:11.3.0", [3000]) +
      `    user: "0"
` +
      data("grafana", "/var/lib/grafana") +
      `    environment:
      GF_SECURITY_ADMIN_USER: admin
      GF_SECURITY_ADMIN_PASSWORD: admin
` +
      labels(web("grafana", "grafana", 3000)),
  },
  {
    id: "mailpit",
    label: "Mailpit",
    group: "tools",
    body: (p) => head(p, "mailpit", "axllent/mailpit:v1.21", [1025, 8025]) + labels(web("mailpit", "mailpit", 8025)),
  },
  {
    id: "adminer",
    label: "Adminer",
    group: "tools",
    body: (p) =>
      head(p, "adminer", "adminer:4", [8080]) +
      `    environment:
      ADMINER_DEFAULT_SERVER: postgres
` +
      labels(web("adminer", "adminer", 8080)),
  },
  {
    id: "pgadmin",
    label: "pgAdmin",
    group: "tools",
    requires: ["postgres"],
    body: (p) =>
      head(p, "pgadmin", "dpage/pgadmin4:8", [80]) +
      `    environment:
      PGADMIN_DEFAULT_EMAIL: admin@example.com
      PGADMIN_DEFAULT_PASSWORD: admin
      PGADMIN_CONFIG_SERVER_MODE: "False"
      PGADMIN_CONFIG_MASTER_PASSWORD_REQUIRED: "False"
` +
      labels(web("pgadmin", "pgadmin", 80)),
  },
  {
    id: "mongo-express",
    label: "Mongo Express",
    group: "tools",
    requires: ["mongo"],
    body: (p) =>
      head(p, "mongo-express", "mongo-express:1.0.2", [8081]) +
      `    depends_on:
      - mongo
    environment:
      ME_CONFIG_MONGODB_URL: mongodb://root:root@mongo:27017/
      ME_CONFIG_BASICAUTH: "false"
` +
      labels(web("mongo-express", "mongo-express", 8081)),
  },
  {
    id: "redisinsight",
    label: "Redis Insight",
    group: "tools",
    body: (p) => head(p, "redisinsight", "redis/redisinsight:2.68", [5540]) + labels(web("redisinsight", "redisinsight", 5540)),
  },
  {
    id: "keycloak",
    label: "Keycloak",
    group: "tools",
    body: (p) =>
      head(p, "keycloak", "quay.io/keycloak/keycloak:26.0", [8080]) +
      `    command: start-dev
    environment:
      KC_BOOTSTRAP_ADMIN_USERNAME: admin
      KC_BOOTSTRAP_ADMIN_PASSWORD: admin
      KC_PROXY_HEADERS: xforwarded
      KC_HTTP_ENABLED: "true"
` +
      labels(web("keycloak", "keycloak", 8080)),
  },
  {
    id: "ollama",
    label: "Ollama",
    group: "tools",
    body: (p) => head(p, "ollama", "ollama/ollama:0.4.1", [11434]) + data("ollama", "/root/.ollama"),
  },
];

export function hasService(compose: string, name: string): boolean {
  const esc = name.replace(/[.*+?^${}()|[\]\\-]/g, "\\$&");
  return new RegExp(`^\\s{2}${esc}:\\s*$`, "m").test(compose);
}

export type InsertResult = { kind: "ok"; text: string; start: number; end: number } | { kind: "exists" };

export function insertService(compose: string, name: string, body: string): InsertResult {
  if (hasService(compose, name)) return { kind: "exists" };
  const lines = compose.replace(/\s+$/, "").split("\n");
  const block = body.replace(/\n+$/, "").split("\n");
  const servicesAt = lines.findIndex((l) => /^services:\s*$/.test(l));
  let at: number;
  if (servicesAt < 0) {
    lines.push("", "services:");
    at = lines.length;
  } else {
    const next = lines.findIndex((l, i) => i > servicesAt && /^[^\s#]/.test(l));
    at = next < 0 ? lines.length : next;
    while (at > servicesAt + 1 && lines[at - 1]!.trim() === "") at--;
  }
  const insert = at > 0 && lines[at - 1]!.trim() !== "" && !/^services:/.test(lines[at - 1]!) ? ["", ...block] : block;
  const before = lines.slice(0, at);
  const after = lines.slice(at);
  const out = [...before, ...insert, ...(after.length && after[0]!.trim() !== "" ? [""] : []), ...after];
  const lead = insert[0] === "" ? 1 : 0;
  const start = before.join("\n").length + (before.length ? 1 : 0) + lead;
  const end = start + insert.join("\n").length - lead;
  return { kind: "ok", text: out.join("\n") + "\n", start, end };
}

export type InsertDepsResult = { kind: "ok"; text: string; start: number; end: number; added: string[] } | { kind: "exists" };

export function insertWithDeps(compose: string, id: string, prefix: string): InsertDepsResult {
  const target = snippets.find((s) => s.id === id);
  if (!target || hasService(compose, target.id)) return { kind: "exists" };
  let text = compose;
  const added: string[] = [];
  let start = -1;
  let end = -1;
  for (const depId of [...(target.requires ?? []), target.id]) {
    const dep = snippets.find((s) => s.id === depId);
    if (!dep || hasService(text, dep.id)) continue;
    const r = insertService(text, dep.id, dep.body(prefix));
    if (r.kind !== "ok") continue;
    text = r.text;
    if (start < 0) start = r.start;
    end = r.end;
    added.push(dep.label);
  }
  return { kind: "ok", text, start, end, added };
}
