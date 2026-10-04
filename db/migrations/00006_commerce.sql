-- 额度、会员、兑换码、支付、邀请、回访（dev-spec 第五节「商业化」「支付」「增长」，PRD 第 13 节）。

-- +goose Up
CREATE TABLE quota_counters (
  owner_user_id BIGINT UNSIGNED NOT NULL,
  quota_type    ENUM('parse_pages','import_questions','grading','ai_questions','paper_grading','essay_grading') NOT NULL,
  period_key    VARCHAR(16)  NOT NULL COMMENT 'total / 2026-10（月）/ 2026-10-01（日）/ 2026-W40（周），按北京时间',
  used          INT UNSIGNED NOT NULL DEFAULT 0,
  reserved      INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '预占未结算（解析）',
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id, quota_type, period_key),
  CONSTRAINT fk_quota_counters_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='额度计数，扣减时 SELECT ... FOR UPDATE 防并发超额';

CREATE TABLE quota_ledger (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id   BIGINT UNSIGNED NOT NULL,
  quota_type      ENUM('parse_pages','import_questions','grading','ai_questions','paper_grading','essay_grading') NOT NULL,
  period_key      VARCHAR(16)  NOT NULL,
  action          ENUM('reserve','settle','release','consume','refund','grant') NOT NULL,
  amount          INT NOT NULL,
  ref_type        VARCHAR(32)  NULL COMMENT 'import_job / grading / paper_session / essay / admin',
  ref_id          BIGINT UNSIGNED NULL,
  idempotency_key VARCHAR(96)  NOT NULL,
  note            VARCHAR(255) NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_quota_ledger_idem (owner_user_id, idempotency_key),
  KEY idx_quota_ledger_owner (owner_user_id, quota_type, created_at),
  CONSTRAINT fk_quota_ledger_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='额度流水；扣额度与写结果在同一事务';

CREATE TABLE memberships (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  owner_user_id BIGINT UNSIGNED NOT NULL,
  tier          ENUM('sprint','season','monthly','gift') NOT NULL COMMENT '冲刺卡 / 考季卡 / 月卡 / 赠送天数',
  source        ENUM('redeem','order','invite','survey','admin') NOT NULL,
  source_ref    BIGINT UNSIGNED NULL,
  starts_at     DATETIME(3)  NOT NULL COMMENT '叠加到当前会员之后',
  ends_at       DATETIME(3)  NOT NULL,
  revoked_at    DATETIME(3)  NULL COMMENT '作废兑换码或退款后收回',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_memberships_owner (owner_user_id, ends_at),
  CONSTRAINT fk_memberships_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='会员时段，当前会员 = 未收回时段里覆盖当前时间的那段';

CREATE TABLE redeem_batches (
  id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name            VARCHAR(64)  NOT NULL,
  tier            ENUM('sprint','season','monthly','gift') NOT NULL,
  days            SMALLINT UNSIGNED NULL COMMENT 'gift 或自定义天数',
  quantity        INT UNSIGNED NOT NULL,
  code_expires_at DATETIME(3)  NOT NULL,
  channel         VARCHAR(32)  NOT NULL DEFAULT '',
  status          ENUM('active','disabled') NOT NULL DEFAULT 'active',
  created_by      BIGINT UNSIGNED NULL,
  created_at      DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='兑换码批次（7.4）';

CREATE TABLE redeem_codes (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  batch_id      BIGINT UNSIGNED NOT NULL,
  code_hash     CHAR(64)     NOT NULL COMMENT '兑换码只存哈希',
  code_tail     CHAR(3)      NOT NULL COMMENT '末 3 位，后台单码查询时辅助核对',
  status        ENUM('unused','used','void') NOT NULL DEFAULT 'unused',
  used_by       BIGINT UNSIGNED NULL,
  used_at       DATETIME(3)  NULL,
  membership_id BIGINT UNSIGNED NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_redeem_codes_hash (code_hash),
  KEY idx_redeem_codes_batch (batch_id, status),
  CONSTRAINT fk_redeem_codes_batch FOREIGN KEY (batch_id) REFERENCES redeem_batches (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE orders (
  id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_no       VARCHAR(32)  NOT NULL,
  owner_user_id  BIGINT UNSIGNED NULL COMMENT '注销后置空，订单保留用于财务',
  tier           ENUM('sprint','season','monthly') NOT NULL,
  channel        ENUM('wechat','alipay','apple_iap') NOT NULL,
  amount_cents   INT UNSIGNED NOT NULL,
  status         ENUM('created','paid','closed','refunding','refunded') NOT NULL DEFAULT 'created',
  transaction_id VARCHAR(64)  NULL,
  notify_hash    CHAR(64)     NULL COMMENT '回调原文哈希',
  membership_id  BIGINT UNSIGNED NULL,
  paid_at        DATETIME(3)  NULL,
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  UNIQUE KEY uk_orders_no (order_no),
  UNIQUE KEY uk_orders_txn (channel, transaction_id),
  KEY idx_orders_owner (owner_user_id),
  KEY idx_orders_status (status, created_at),
  CONSTRAINT fk_orders_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='订单（支付开关打开后使用）';

CREATE TABLE refunds (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  order_id    BIGINT UNSIGNED NOT NULL,
  reason      VARCHAR(255) NOT NULL,
  status      ENUM('pending','approved','rejected','done') NOT NULL DEFAULT 'pending',
  handled_by  BIGINT UNSIGNED NULL,
  handled_at  DATETIME(3)  NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id),
  KEY idx_refunds_order (order_id),
  CONSTRAINT fk_refunds_order FOREIGN KEY (order_id) REFERENCES orders (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE invites (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  inviter_id   BIGINT UNSIGNED NOT NULL,
  invitee_id   BIGINT UNSIGNED NOT NULL,
  registered_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  activated_at DATETIME(3)  NULL COMMENT '被邀请人导入第一份资料的时间，此时发奖励',
  inviter_days SMALLINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '邀请人实际得到的天数（受 70 天上限）',
  PRIMARY KEY (id),
  UNIQUE KEY uk_invites_invitee (invitee_id),
  KEY idx_invites_inviter (inviter_id),
  CONSTRAINT fk_invites_inviter FOREIGN KEY (inviter_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_invites_invitee FOREIGN KEY (invitee_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='邀请研友（6.8）';

CREATE TABLE survey_responses (
  owner_user_id  BIGINT UNSIGNED NOT NULL,
  exam_year      SMALLINT UNSIGNED NOT NULL,
  scores         JSON         NOT NULL COMMENT '每门课实际成绩与考前预估',
  retest_result  ENUM('in','out','unknown') NOT NULL,
  admission      ENUM('admitted','adjusted','rejected','pending') NULL,
  share_consent  TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '同意作为匿名上岸案例',
  membership_id  BIGINT UNSIGNED NULL COMMENT '赠送的 30 天会员',
  created_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at     DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (owner_user_id),
  CONSTRAINT fk_survey_responses_owner FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='考后回访（6.14），每人一次';

-- +goose Down
DROP TABLE survey_responses;
DROP TABLE invites;
DROP TABLE refunds;
DROP TABLE orders;
DROP TABLE redeem_codes;
DROP TABLE redeem_batches;
DROP TABLE memberships;
DROP TABLE quota_ledger;
DROP TABLE quota_counters;
