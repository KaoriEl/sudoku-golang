export interface Snippet {
  id: string;
  label: string;
  body: (prefix: string) => string;
}

export function detectPrefix(compose: string): string {
  const m = /container_name:\s*["']?([^\s"'$]+)_[a-z0-9-]+["']?\s*$/m.exec(compose);
  return m?.[1] ?? "dev";
}

export const snippets: Snippet[] = [
  {
    id: "rabbitmq",
    label: "RabbitMQ",
    body: (p) => `  rabbitmq:
    image: rabbitmq:3.13-management
    container_name: ${p}_rabbitmq
    hostname: rabbitmq
    networks:
      - sudoku_network
    expose:
      - "5672/tcp"
      - "15672/tcp"
    restart: unless-stopped
    volumes:
      - ./sudoku-config/rabbitmq/data:/var/lib/rabbitmq:delegated
    environment:
      RABBITMQ_DEFAULT_USER: rabbitmq
      RABBITMQ_DEFAULT_PASS: rabbitmq
    labels:
      - "traefik.enable=true"
      - "traefik.tcp.routers.rabbitmq.entrypoints=rabbitmq"
      - "traefik.tcp.routers.rabbitmq.rule=HostSNI(\`*\`)"
      - "traefik.tcp.routers.rabbitmq.service=rabbitmq"
      - "traefik.tcp.services.rabbitmq.loadBalancer.server.port=5672"
      - "traefik.http.routers.rabbitmq-mgmt.entrypoints=web_80"
      - "traefik.http.routers.rabbitmq-mgmt.rule=Host(\`rabbitmq.localhost\`)"
      - "traefik.http.routers.rabbitmq-mgmt.service=rabbitmq-mgmt"
      - "traefik.http.services.rabbitmq-mgmt.loadBalancer.server.port=15672"
`,
  },
  {
    id: "postgres",
    label: "PostgreSQL",
    body: (p) => `  postgres:
    image: postgres:17-alpine
    container_name: ${p}_postgres
    hostname: postgres
    networks:
      - sudoku_network
    expose:
      - "5432/tcp"
    restart: unless-stopped
    volumes:
      - ./sudoku-config/postgres/data:/var/lib/postgresql/data:delegated
    environment:
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
    body: (p) => `  mysql:
    image: mysql:8.4
    container_name: ${p}_mysql
    hostname: mysql
    networks:
      - sudoku_network
    expose:
      - "3306/tcp"
    restart: unless-stopped
    volumes:
      - ./sudoku-config/mysql/data:/var/lib/mysql:delegated
    environment:
      MYSQL_ROOT_PASSWORD: root
    healthcheck:
      test: ["CMD", "mysqladmin", "ping", "-proot"]
      interval: 5s
      retries: 10
`,
  },
  {
    id: "mongo",
    label: "MongoDB",
    body: (p) => `  mongo:
    image: mongo:8
    container_name: ${p}_mongo
    hostname: mongo
    networks:
      - sudoku_network
    expose:
      - "27017/tcp"
    restart: unless-stopped
    volumes:
      - ./sudoku-config/mongo/data:/data/db:delegated
    environment:
      MONGO_INITDB_ROOT_USERNAME: root
      MONGO_INITDB_ROOT_PASSWORD: root
`,
  },
  {
    id: "mailpit",
    label: "Mailpit",
    body: (p) => `  mailpit:
    image: axllent/mailpit:latest
    container_name: ${p}_mailpit
    hostname: mailpit
    networks:
      - sudoku_network
    expose:
      - "1025/tcp"
      - "8025/tcp"
    restart: unless-stopped
    labels:
      - "traefik.enable=true"
      - "traefik.http.routers.mailpit.entrypoints=web_80"
      - "traefik.http.routers.mailpit.rule=Host(\`mailpit.localhost\`)"
      - "traefik.http.services.mailpit.loadBalancer.server.port=8025"
`,
  },
];

export type InsertResult = { kind: "ok"; text: string; start: number; end: number } | { kind: "exists" };

export function insertService(compose: string, name: string, body: string): InsertResult {
  const lines = compose.replace(/\s+$/, "").split("\n");
  if (lines.some((l) => new RegExp(`^\\s{2}${name}:\\s*$`).test(l))) return { kind: "exists" };
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
  const start = before.join("\n").length + (before.length ? 1 : 0) + (insert[0] === "" ? 1 : 0);
  const end = start + insert.join("\n").length;
  return { kind: "ok", text: out.join("\n") + "\n", start, end };
}
