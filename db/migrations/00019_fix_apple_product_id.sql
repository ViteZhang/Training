-- +goose Up
-- 域名与包名统一为 dreamerlab：已跑过 00015 的库里内购商品 ID 还是旧拼写 cn.dreamelab.*，这里改成新的。
-- 新库跑 00015 时已经是新拼写，这里再写一遍结果不变。
UPDATE rule_params SET value = JSON_SET(value,
  '$.sprint.apple_product_id', 'cn.dreamerlab.training.sprint',
  '$.season.apple_product_id', 'cn.dreamerlab.training.season',
  '$.monthly.apple_product_id', 'cn.dreamerlab.training.monthly')
WHERE param_key = 'pricing';

-- +goose Down
UPDATE rule_params SET value = JSON_SET(value,
  '$.sprint.apple_product_id', 'cn.dreamelab.training.sprint',
  '$.season.apple_product_id', 'cn.dreamelab.training.season',
  '$.monthly.apple_product_id', 'cn.dreamelab.training.monthly')
WHERE param_key = 'pricing';
