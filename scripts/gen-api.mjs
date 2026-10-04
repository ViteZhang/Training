// 拉取后端接口契约并重新生成 packages/api-client（CLAUDE.md「常用命令」pnpm gen:api）。
//
// 契约来源（按优先级）：
//   1. API_SPEC_PATH：本地文件路径（开发暂存仓库里两个目录并排时，默认 ../peetraining-server/api/openapi.yaml）
//   2. API_SPEC_REPO + API_SPEC_REF：后端仓库地址与契约标签（如 api-v0.1），用 git 浅克隆该标签取文件
// 拉到的契约写入 packages/api-client/openapi.yaml 并提交，生成结果可复现。
import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const target = join(root, 'packages/api-client/openapi.yaml');
const localDefault = resolve(root, '../peetraining-server/api/openapi.yaml');

const { API_SPEC_PATH, API_SPEC_REPO, API_SPEC_REF } = process.env;

if (API_SPEC_REPO && API_SPEC_REF) {
  const dir = mkdtempSync(join(tmpdir(), 'api-spec-'));
  try {
    execFileSync('git', ['clone', '--depth=1', '--branch', API_SPEC_REF, API_SPEC_REPO, dir], { stdio: 'inherit' });
    copyFileSync(join(dir, 'api/openapi.yaml'), target);
    console.log(`契约来自 ${API_SPEC_REPO}@${API_SPEC_REF}`);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
} else {
  const src = API_SPEC_PATH ? resolve(API_SPEC_PATH) : localDefault;
  if (!existsSync(src)) {
    console.error(`找不到契约文件 ${src}。请设置 API_SPEC_PATH，或 API_SPEC_REPO 与 API_SPEC_REF。`);
    process.exit(1);
  }
  copyFileSync(src, target);
  console.log(`契约来自 ${src}`);
}

execFileSync('pnpm', ['--filter', '@training/api-client', 'generate'], { stdio: 'inherit', cwd: root });
