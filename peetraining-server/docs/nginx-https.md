# 现有 Nginx 接入 Training API 与 HTTPS

本说明基于你提供的 nginx.conf 和基础服务 Compose。现有 `dreamer-nginx` 已映射宿主机 `80:80`、`443:443`，并加入 `dreamer-net`。Training API 只在 Docker 网络中使用 `training-api:8080`，不需要新增宿主机端口映射，也不需要启动第二个 Nginx。Java 服务容器的 8080 与 API 容器的 8080 互不冲突。

生产域名已确定为 `training.dreamerlab.cn`，使用你现有的 `dreamerlab.cn` 主域名。本文和 `deploy/nginx/training.conf` 中的域名、证书文件名已统一。

## 1. DNS 与证书

在你实际持有的域名的 DNS 控制台，为 `training` 添加 A 记录，指向应用 ECS 公网 IP `154.9.254.163`。ECS 安全组及系统防火墙允许 TCP 80、443；无需开放 API 的 8080。

在阿里云数字证书管理服务申请覆盖完整域名的证书，按控制台要求添加 DNS 验证记录，签发后下载 **Nginx 格式**证书。现有 `dreamerai.net` 或主域名证书不一定覆盖新域名；核对证书的 SAN 域名列表。下载包应包含 `.pem` 证书链和 `.key` 私钥。

## 2. 确认服务器上的真实挂载目录

在 ECS 执行：

```bash
docker inspect dreamer-nginx --format '{{range .Mounts}}{{printf "%s -> %s\n" .Source .Destination}}{{end}}'
```

你提供的基础 Compose 中有以下挂载：

```yaml
volumes:
  - ./nginx/conf/nginx.conf:/etc/nginx/nginx.conf
  - ./nginx/cert:/etc/nginx/cert
```

左侧是宿主机文件/目录，右侧是 Nginx 容器内路径。`./nginx` 相对的是基础服务 Compose 文件所在目录，不是 Training 目录。以上 `docker inspect` 输出能给出实际的绝对路径，不要猜测。

把证书和私钥上传到映射至 `/etc/nginx/cert` 的宿主机目录，分别命名为：

```text
training.dreamerlab.cn.pem
training.dreamerlab.cn.key
```

让 Nginx 主进程可以读取私钥，并限制其他用户读取。不要把生产私钥提交到仓库。后续续期时在该挂载目录替换证书，再执行下文的校验和 reload。

## 3. 添加配置

打开映射至 `/etc/nginx/nginx.conf` 的宿主机文件，把仓库 `deploy/nginx/training.conf` 的两个 `server { ... }` 块追加到现有 **`http { ... }` 内、最后一个右花括号之前**。已有站点的 server/upstream 保留。

多个域名在同一个 Nginx 里监听 80/443 是正常配置，Nginx 按域名和 HTTPS SNI 区分站点。不要把该片段放到 `http` 外，也不要用片段覆盖完整 nginx.conf。

片段将本域名全部路径代理到后端，完整保留 `/api/v1/...` 和参数。`/` 返回 404 不代表代理失败，应访问 `/api/v1/health`。本次只配置后端 API；管理后台 `/admin/` 的静态网页还需另行部署和配置。生产环境默认不提供本地开发用的 Swagger `/docs`。

使用 Docker 内置 DNS `127.0.0.11` 动态解析 `training-api`，API 每次重新部署后无需为容器 IP 变化重启 Nginx。配置中的证书路径均为容器内路径。

## 4. 检查网络、校验配置并生效

```bash
docker network inspect dreamer-net --format '{{range .Containers}}{{println .Name}}{{end}}'
```

输出需包含 `dreamer-nginx` 和 `training-api`。你提供的两份 Compose 已配置该网络；若 Nginx 实际未连接，再手动执行 `docker network connect dreamer-net dreamer-nginx`，并在基础 Compose 中保留网络配置。

在 ECS 执行：

```bash
docker exec dreamer-nginx nginx -t && docker exec dreamer-nginx nginx -s reload
```

只在 `nginx -t` 成功时 reload，复用现有网关，不重新占用 80/443。若提示证书文件不存在，检查挂载目录和文件名；若报 `key values mismatch`，需使用同一次签发下载的配对证书和私钥。

## 5. 验证

```bash
# HTTP 应返回 301，并跳转到同域名 HTTPS。
curl -I http://training.dreamerlab.cn/api/v1/health

# 正常应返回 HTTP 200，且 MySQL、Redis 健康。
curl -i https://training.dreamerlab.cn/api/v1/health

# DNS 尚未生效时，可从本地指定 IP 验证域名与证书；证书仍需已签发并正确安装。
curl --resolve training.dreamerlab.cn:443:154.9.254.163 https://training.dreamerlab.cn/api/v1/health
```

不要用 `-k` 跳过证书校验来判定 HTTPS 正常。502 时检查 `docker logs --tail 100 dreamer-nginx` 和 `docker logs --tail 100 training-api`；503 且返回应用健康 JSON 时，继续排查 API 的数据库/Redis 连接。

这里的 DNS、证书上传和 Nginx reload 是首次接入/续期步骤，原有云效 Go 构建、镜像推送和 Docker 部署节点无需改变。
