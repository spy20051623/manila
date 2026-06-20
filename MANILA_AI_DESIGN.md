# 马尼拉 AI 参数设计

本文档定义马尼拉 AI 的决策参数。目标是先做可解释的参数化 AI，再把同一套特征和权重迁移到 Python 训练。

当前阶段不设计复杂神经网络结构，只定义：

- AI 关注哪些参数。
- 参数怎样影响动作选择。
- 如何用这些参数实现 Random、Greedy、MonteCarlo 和后续学习型 AI。

根据已有讨论记录，第一版 AI 参数采用更收敛的 18 维可进化参数方案：

- 所有输入特征先归一化。
- 期望收益权重固定为 1，不作为可训练参数。
- 其他参数表示相对期望收益的偏好强度。
- 参数基因统一存储为 `[0,1]`，使用时映射到实际值域。
- 决策不强制选择最高分，可通过 softmax temperature 控制随机性。
- 骰子概率、费用、奖励、规则收益等客观规则不作为可进化参数，应由规则/概率模块精确或近似计算。

## 1. AI 总体决策模型

AI 每次行动时只从后端返回的合法动作中选择。

推荐统一使用打分模型：

```text
score(action) =
    expected_value
  + w_cash * cash_safety
  + w_flexibility * future_flexibility
  + w_own_asset * own_asset_gain
  + w_opponent_denial * opponent_denial
  + w_leader_denial * leader_denial
  + effective_risk * return_variance
  + w_upside * upside
  - w_tail_loss * tail_loss
  - w_uncertainty * uncertainty
  + action_type_bias
```

其中：

- `state` 是当前完整游戏状态。
- `action` 是一个合法动作。
- `expected_value` 是动作期望收益，权重固定为 1。
- 其他特征均应归一化到 `[-1,1]` 或 `[0,1]`。
- `action_type_bias` 第一版可为 0，第二阶段再加入。

AI 永远不自行生成非法动作。动作合法性只由 Go 后端规则引擎决定。

动作选择使用 softmax：

```text
P(action) = softmax(score / temperature)
```

`temperature` 越低越接近纯贪心；越高越随机。

## 2. 全局风格参数

第一版使用 18 个可进化参数。它们分为通用决策参数、竞价参数、局势调整参数和动作专项参数。

### 2.1 通用决策参数

| 参数 | 建议值域 | 初始值 | 含义 |
| --- | ---: | ---: | --- |
| `w_cash` | 0-2.0 | 0.6 | 保留现金的价值 |
| `w_flexibility` | 0-1.5 | 0.4 | 保留后续选择空间的价值 |
| `w_own_asset` | 0-2.5 | 1.0 | 行动与自己持有货物/股份的协同 |
| `w_opponent_denial` | 0-2.0 | 0.35 | 阻止其他玩家获利 |
| `w_leader_denial` | 0-1.5 | 0.25 | 专门针对当前领先者 |
| `w_variance` | -1.25-1.25 | 0 | 对波动性的偏好，负数保守，正数冒险 |
| `w_tail_loss` | 0-2.5 | 0.6 | 对最坏结果的厌恶 |
| `w_upside` | 0-1.5 | 0.2 | 对高额回报可能性的额外偏好 |
| `w_uncertainty` | 0-1.0 | 0.3 | 对估算不准的折价 |
| `temperature` | 0.05-0.8 | 0.2 | 决策随机程度，使用对数映射 |

不设置 `w_expected_value`，固定为：

```text
w_expected_value = 1.0
```

这样可以避免所有权重同比例放大/缩小后与 `temperature` 产生等价行为，减少无效搜索空间。

### 2.2 竞价参数

| 参数 | 建议值域 | 初始值 | 含义 |
| --- | ---: | ---: | --- |
| `bid_value_multiplier` | 0.65-1.35 | 0.95 | 对港务长控制权估值的修正 |
| `bid_cash_cap` | 0.20-0.75 | 0.45 | 最多愿意拿当前现金的多少比例竞价 |
| `reserve_cash_ratio` | 0.05-0.40 | 0.15 | 竞价后至少保留多少起始资金比例 |
| `bid_block_weight` | 0-1.2 | 0.15 | 为阻止对手获得控制权而额外加价 |

最高可接受出价：

```text
max_bid = min(
    control_value * bid_value_multiplier
  + denial_value * bid_block_weight,
    current_cash * bid_cash_cap,
    current_cash - starting_cash * reserve_cash_ratio
)
```

如果 `max_bid >= currentBid + 1`，AI 可以竞价；否则放弃竞拍。

### 2.3 局势调整参数

| 参数 | 建议值域 | 初始值 | 含义 |
| --- | ---: | ---: | --- |
| `leading_risk_shift` | -1.25-0.5 | -0.35 | 领先时对风险偏好的调整 |
| `trailing_risk_shift` | -0.5-1.5 | 0.45 | 落后时对风险偏好的调整 |
| `late_game_risk_shift` | -1.0-1.0 | 0.1 | 后期更保守或更冒险 |

实际风险偏好：

```text
effective_risk =
    w_variance
  + leading_ratio * leading_risk_shift
  + trailing_ratio * trailing_risk_shift
  + game_phase * late_game_risk_shift
```

其中：

```text
game_phase = currentRound / estimatedMaxRounds
```

## 3. 通用状态特征

以下特征可用于所有动作打分。所有特征必须归一化，否则参数值域没有意义。

| 特征 | 说明 | 影响 |
| --- | --- | --- |
| `cashAfterAction` | 执行动作后的现金 | 现金越低，保守 AI 越扣分 |
| `mortgageCountAfterAction` | 动作后抵押股份数 | 保守 AI 扣分 |
| `bankruptRisk` | 本轮或下轮进入破产的风险 | 保守 AI 扣分 |
| `expectedImmediateCash` | 本轮可能获得的现金期望 | 即时收益 AI 加分 |
| `expectedMarketGain` | 动作导致持股货物升值的收益期望 | 长期 AI 加分 |
| `opponentExpectedGain` | 对手可能因此获利 | 阻挡型 AI 扣分 |
| `rankPressure` | 当前排名压力 | 落后时提高风险权重 |

`rankPressure` 可以用当前估算财富排名计算：

```text
estimatedWealth = cash + Σ shares[goods] * marketValue(goods) - mortgagedShares * 15
```

### 3.1 金钱尺度

所有和金钱有关的收益建议除以固定 `money_scale`：

```text
ev_normalized = clip(expected_profit / money_scale, -1, 1)
```

`money_scale` 不应每轮动态变化。推荐做法：

1. 用 RandomAI 和基础 GreedyAI 跑几千局。
2. 收集动作收益估计的绝对值。
3. 取 90%-95% 分位数。
4. 固定为 `money_scale`。

例如如果 95% 的单次动作价值在 12 比索以内：

```text
money_scale = 12
```

### 3.2 现金安全度

```text
cash_safety = clip((cash_after_action - desired_reserve) / starting_cash, -1, 1)
```

### 3.3 自己资产收益

```text
own_asset_gain = clip(expected_change_to_my_assets / money_scale, -1, 1)
```

例如某货物升值会让 AI 的股份总价值增加 10，就把这部分计入。

### 3.4 阻止对手获利

```text
opponent_denial = clip(
    (opponent_best_value_before_action - opponent_best_value_after_action) / money_scale,
    0,
    1
)
```

`leader_denial` 使用同类公式，但只针对当前估算财富最高的玩家。

### 3.5 风险、上行和尾部损失

风险不要只看方差，需要同时看最坏尾部：

```text
return_variance = clip(stddev(outcome) / money_scale, 0, 1)
tail_loss = clip(-average(worst_20_percent_outcomes) / money_scale, 0, 1)
upside = clip(average(best_20_percent_outcomes) / money_scale, 0, 1)
```

这样可以区分：

- 波动大，但失败损失有限。
- 波动大，而且失败会破产。

### 3.6 未来灵活度

```text
future_flexibility = reasonable_future_action_count / maximum_possible_future_action_count
```

范围为 `[0,1]`。

### 3.7 领先和落后程度

```text
score_gap = my_estimated_wealth - table_average_estimated_wealth
leading_ratio = clip(score_gap / target_final_score, 0, 1)
trailing_ratio = clip(-score_gap / target_final_score, 0, 1)
```

`target_final_score` 第一版可以先取 80，后续用自博弈数据修正。

## 4. 竞拍港务长参数

竞拍是全局战略动作，影响买股、选货、设船起点。

### 4.1 关键参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `harborMasterValueBase` | 港务长基础价值 | 更愿意参与竞拍 |
| `harborMasterShareBonus` | 买股机会价值 | 有想买股份时提高出价 |
| `harborMasterCargoControlBonus` | 控制出航货物价值 | 持股集中时提高出价 |
| `harborMasterStartControlBonus` | 控制船起点价值 | 更重视调整到港概率 |
| `bidAggression` | 竞价激进度 | 更接近最大可支付能力 |
| `bidSafetyMargin` | 竞价安全边际 | 保留更多现金 |

### 4.2 出价逻辑

AI 先估算港务长价值：

```text
hmValue =
  harborMasterValueBase
  + harborMasterShareBonus * bestShareValue
  + harborMasterCargoControlBonus * cargoControlValue
  + harborMasterStartControlBonus * startControlValue
```

然后决定是否出价：

```text
if currentBid + 1 <= hmValue and currentBid + 1 <= maxPayable:
    bid
else:
    pass
```

出价金额：

```text
bidAmount = min(maxPayable - safetyReserve, currentBid + bidIncrement)
```

`bidAggression` 越高，`bidIncrement` 越大，`safetyReserve` 越小。

## 5. 买股份参数

### 5.1 关键参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `shareExpectedValueWeight` | 股份期望收益权重 | 更愿意买预计升值货物 |
| `shareDiversificationWeight` | 分散持股偏好 | 更愿意买自己没有的货物 |
| `shareSynergyWeight` | 持股集中偏好 | 更愿意加仓已有货物 |
| `sharePriceSensitivity` | 股价敏感度 | 更不愿意买贵股份 |
| `endgameShareUrgency` | 终局股份紧迫度 | 货物接近 30 时更重视股份 |

### 5.2 股份评分

```text
shareScore(goods) =
  expectedMarketAdvance(goods) * shareExpectedValueWeight
  + ownShares(goods) * shareSynergyWeight
  + noOwnShares(goods) * shareDiversificationWeight
  - price(goods) * sharePriceSensitivity
  + nearEndgame(goods) * endgameShareUrgency
```

如果所有 `shareScore <= 0`，AI 可以跳过买股。

## 6. 选择出航货物参数

港务长选择 3 种货物出航，等于决定哪种货物本轮有机会升值。

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `ownShareCargoWeight` | 自己持股货物出航偏好 | 更常让自己持股货物出航 |
| `opponentShareDenyWeight` | 阻止对手持股货物偏好 | 更常排除对手重仓货物 |
| `highValueCargoWeight` | 高价值货物偏好 | 更常推动接近终局的货物 |
| `lowValueCargoSpeculation` | 低价货物投机偏好 | 更常推动低价货物追涨 |

排除某货物的评分：

```text
excludeScore(goods) =
  opponentShares(goods) * opponentShareDenyWeight
  - ownShares(goods) * ownShareCargoWeight
  - nearEndgame(goods) * highValueCargoWeight
```

AI 排除 `excludeScore` 最高的货物。

## 7. 设置船起点参数

船起点影响到港概率、海盗触发概率和船坞概率。

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `favoredCargoAdvance` | 偏好货物前推 | 自己持股/押注货物起点更高 |
| `dangerCargoDelay` | 对手货物后置 | 对手持股货物起点更低 |
| `pirateSetupPreference` | 海盗局面偏好 | 更愿意制造停 13 的可能 |
| `dockSetupPreference` | 船坞局面偏好 | 更愿意让某船难到港 |

起点组合必须满足：

```text
每船 0-5，三船总和 9
```

AI 应枚举所有合法组合，对组合打分。

## 8. 放置同伙参数

放置是最常见决策，需要按位置类型打分。

### 8.1 货船参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `shipExpectedPayoutWeight` | 货船收益期望 | 更爱上高期望船 |
| `shipCostSensitivity` | 登船费用敏感度 | 更少上贵船 |
| `shipMarketAdvanceWeight` | 货物升值收益 | 持股货物更想上船 |
| `shipCrowdingPenalty` | 船上人数惩罚 | 避免收益被平分 |
| `shipPirateRiskPenalty` | 海盗风险惩罚 | 避免可能被掠夺的船 |

```text
shipScore =
  arrivalProbability * payoutPerCrew * shipExpectedPayoutWeight
  + arrivalProbability * ownShares(goods) * marketAdvanceValue * shipMarketAdvanceWeight
  - cost * shipCostSensitivity
  - crewCount * shipCrowdingPenalty
  - pirateRisk * shipPirateRiskPenalty
```

### 8.2 港口参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `portExpectedValueWeight` | 港口收益期望 | 更愿意押到港顺序 |
| `portCostSensitivity` | 港口费用敏感度 | 更少押贵港口 |
| `portLateRewardPreference` | 高倍率偏好 | 更愿意押 C 港 |

```text
portScore =
  probabilityNthArrival(slot) * reward(slot) * portExpectedValueWeight
  - cost(slot) * portCostSensitivity
```

### 8.3 船坞参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `dockExpectedValueWeight` | 船坞收益期望 | 更愿意押沉船 |
| `dockCostSensitivity` | 船坞费用敏感度 | 更少押贵船坞 |
| `dockInsuranceAwareness` | 保险相关意识 | 有保险员时更敢押船坞 |

```text
dockScore =
  probabilityNthDock(slot) * reward(slot) * dockExpectedValueWeight
  - cost(slot) * dockCostSensitivity
```

### 8.4 海盗参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `pirateLootPreference` | 掠夺偏好 | 更愿意上海盗船 |
| `pirateControlValue` | 船长控制价值 | 更重视海盗船长位置 |
| `pirateBoardingFlexibility` | 第 2 次登船灵活性 | 更重视可转船员的机会 |
| `pirateCostSensitivity` | 海盗费用敏感度 | 更少花 5 上海盗 |

```text
pirateScore =
  probabilityShipAt13 * expectedLoot * pirateLootPreference
  + isCaptainSlot * pirateControlValue
  + boardingOpportunity * pirateBoardingFlexibility
  - 5 * pirateCostSensitivity
```

### 8.5 领航员参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `navigator_value_multiplier` | 购买领航员价值倍率，建议值域 0.2-1.8，旧 genome 默认 1.0 | 更愿意放领航员 |
| `navigatorOwnCargoBoost` | 推己方货物 | 更常把自己货物推到港 |
| `navigatorOpponentBlock` | 阻对手货物 | 更常把对手货物往后拉 |
| `navigatorPirateSetup` | 制造/避开 13 | 更主动操控海盗风险 |

领航员评分要考虑第 3 次掷骰前的局面价值。

### 8.6 保险参数

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `insuranceCashNeed` | 现金需求 | 缺钱时更想拿保险 |
| `insuranceRiskTolerance` | 赔付风险容忍 | 更敢承担船坞赔付 |
| `insuranceExpectedLossWeight` | 预期赔付敏感度 | 船坞风险高时更少买保险 |

```text
insuranceScore =
  10 * insuranceCashNeed
  - expectedDockPayoutLiability * insuranceExpectedLossWeight
```

## 9. 海盗登船与掠夺参数

### 9.1 第 2 次移动后登船

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `pirateBecomeCrewValue` | 从海盗变船员价值 | 更愿意登上可收益货船 |
| `pirateKeepCaptainValue` | 保留海盗身份价值 | 船长更不愿意登船 |
| `pirateMatePromotionValue` | 让副手接任价值 | 有副手时船长更敢登船 |

```text
boardScore =
  expectedCrewPayoutAfterBoard * pirateBecomeCrewValue
  - lostPirateControlValue * pirateKeepCaptainValue
  + mateCanPromote * pirateMatePromotionValue
```

### 9.2 第 3 次移动后掠夺去向

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `lootSendPortOwnShareWeight` | 掠夺后送港推动自己股份 | 更愿意让自己持股货物升值 |
| `lootSendDockBlockWeight` | 掠夺后送船坞阻止升值 | 更愿意阻止对手持股货物 |
| `lootImmediateCashWeight` | 海盗现金收益 | 更看重当前掠夺收入 |

目的地选择：

```text
sendPortScore =
  ownShares(goods) * marketAdvanceValue * lootSendPortOwnShareWeight
  - opponentShares(goods) * marketAdvanceValue * lootSendDockBlockWeight

sendDockScore =
  opponentShares(goods) * marketAdvanceValue * lootSendDockBlockWeight
```

选择分数更高的去向。

## 10. 领航员移动参数

领航员动作需要枚举所有合法移动组合，然后打分。当前 Go 第一版实现已经使用这种方式，而不是对“移动”动作给固定分。

枚举范围：

- 小领航员：选择一艘仍在航行的船，移动 `-1` 或 `+1`。
- 大领航员：可以选择一艘船移动 `-2/-1/+1/+2`，也可以选择两艘不同的船各移动 `-1/+1`，总移动绝对值不超过 2。
- 如果所有移动组合的收益提升都很低，AI 会选择不移动。

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `moveOwnShipToPort` | 推己方船到港 | 更愿意让自己持股/押注船到港 |
| `moveOpponentShipBack` | 拉对手船后退 | 更愿意阻止对手收益 |
| `moveShipToPirate13` | 制造 13 海盗点 | 有海盗优势时更愿意 |
| `avoidPirate13` | 避免 13 海盗点 | 自己船有风险时更愿意 |
| `moveDockBetSupport` | 支持船坞押注 | 自己押船坞时更愿意让船失败 |

```text
navigatorMoveScore =
  boardValue(afterMove) - boardValue(beforeMove)
```

`boardValue` 当前包含以下轻量估值：

```text
boardValue =
  expectedCrewPayoutForMe
  - discountedExpectedCrewPayoutForOpponents
  + expectedMarketAdvanceValueForMyShares
  - expectedMarketAdvanceValueForLeaderAndOpponents
  + expectedPortDockSlotValue
  + expectedPirateControlValue
  - expectedPirateRiskToMyCrew
```

因此领航员不会只看单艘船上有没有自己人，还会看：

- 这次移动是否提高/降低货船到港概率。
- 这次移动是否改变港口、船坞 A/B/C 的命中概率。
- 这次移动是否让自己持股货物更容易升值。
- 这次移动是否帮助了领先玩家的船员收益或股份收益。
- 这次移动是否制造或避开海盗停 13 的风险。

## 11. 破产与现金参数

AI 不主动抵押，系统自动抵押。因此现金参数只影响“是否愿意付费”。

| 参数 | 含义 | 值越高的行为 |
| --- | --- | --- |
| `avoidMortgageWeight` | 避免抵押 | 更少执行会触发抵押的动作 |
| `avoidBankruptWeight` | 避免破产 | 更保守地保留现金 |
| `stowawayAcceptance` | 接受偷渡 | 更能接受破产后偷渡 |

注意：

- 抵押哪种股份不影响终局财富，因此不需要作为策略参数。
- 破产状态本轮锁定，会限制后续放置，所以应显著扣分。

## 12. 概率估计参数

早期 AI 可以使用简单概率估计，不必完全精确。

| 参数 | 含义 |
| --- | --- |
| `arrivalProbability(ship)` | 船最终到港概率 |
| `dockProbability(ship)` | 船最终进船坞概率 |
| `pirate13Probability(ship)` | 船停在 13 的概率 |
| `nthArrivalProbability(slot)` | 第 N 艘到港概率 |
| `nthDockProbability(slot)` | 第 N 艘进船坞概率 |

实现方式分三档：

1. **简单启发式**：根据当前位置和剩余骰数估算。
2. **枚举骰点**：枚举剩余 1-3 次骰子结果。
3. **蒙特卡洛采样**：随机模拟若干后续。

第一版 GreedyAI 推荐用简单启发式。MonteCarloAI 再用采样。

## 13. 推荐参数配置

### 13.1 RandomAI

不使用参数，直接从合法动作均匀随机选择。

### 13.2 GreedyAI

推荐初始配置：

```text
w_cash = 0.6
w_flexibility = 0.4
w_own_asset = 1.0
w_opponent_denial = 0.35
w_leader_denial = 0.25
w_variance = 0.0
w_tail_loss = 0.6
w_upside = 0.2
w_uncertainty = 0.3
temperature = 0.2
bid_value_multiplier = 0.95
bid_cash_cap = 0.45
reserve_cash_ratio = 0.15
bid_block_weight = 0.15
leading_risk_shift = -0.35
trailing_risk_shift = 0.45
late_game_risk_shift = 0.1
```

特点：

- 优先选择即时收益为正的动作。
- 会买便宜且有升值机会的股份。
- 不太主动阻挡对手。
- 尽量避免破产。

### 13.3 AggressiveAI

```text
w_cash = 0.25
w_flexibility = 0.2
w_own_asset = 0.8
w_opponent_denial = 0.55
w_leader_denial = 0.35
w_variance = 0.75
w_tail_loss = 0.25
w_upside = 0.8
w_uncertainty = 0.15
temperature = 0.28
bid_value_multiplier = 1.15
bid_cash_cap = 0.65
reserve_cash_ratio = 0.05
bid_block_weight = 0.45
leading_risk_shift = -0.15
trailing_risk_shift = 0.85
late_game_risk_shift = 0.25
```

特点：

- 高价竞拍更多。
- 更爱海盗、高收益港口和船坞。
- 更容易触发抵押。

### 13.4 InvestorAI

```text
w_cash = 0.75
w_flexibility = 0.55
w_own_asset = 1.75
w_opponent_denial = 0.25
w_leader_denial = 0.25
w_variance = -0.35
w_tail_loss = 0.75
w_upside = 0.15
w_uncertainty = 0.35
temperature = 0.12
bid_value_multiplier = 1.05
bid_cash_cap = 0.40
reserve_cash_ratio = 0.20
bid_block_weight = 0.10
leading_risk_shift = -0.55
trailing_risk_shift = 0.30
late_game_risk_shift = -0.05
```

特点：

- 重视买股和推动自己持股货物升值。
- 港务长估值更高。
- 更愿意选择自己持股货物出航。

## 14. 训练时的参数化方式

训练分两条线。

### 14.1 可解释参数训练

第一阶段把 18 个参数作为一个向量：

```text
theta = [w_cash, w_flexibility, w_own_asset, ..., late_game_risk_shift]
```

训练目标：

```text
maximize average final rank reward
```

可用方法：

- 随机搜索。
- 进化策略。
- 贝叶斯优化。
- CMA-ES。

优点：

- 容易解释。
- 训练成本低。
- 适合先得到强于 RandomAI 的 baseline。

### 14.2 基因归一化

训练内部所有基因统一存储为：

```text
gene ∈ [0,1]
```

使用时再映射到实际值域。

线性映射：

```text
decode_linear(gene, low, high) = low + gene * (high - low)
```

例如：

```text
w_variance = decode_linear(gene, -1.25, 1.25)
bid_cash_cap = decode_linear(gene, 0.20, 0.75)
```

`temperature` 使用对数映射：

```text
decode_log(gene, low, high) =
    exp(log(low) + gene * (log(high) - log(low)))
```

原因是 `0.05 -> 0.10` 的行为差异通常大于 `0.70 -> 0.75`。

### 14.3 不归一化权重总和

不要强制：

```text
sum(weights) == 1
```

原因：

- 有些权重允许为负，例如 `w_variance`。
- 现金、安全、风险、阻挡不是互斥比例。
- 整体权重大小会和 `temperature` 共同影响确定性。
- 强制加总会导致增加一个偏好时被迫压低其他偏好。

采用：

```text
expected_value 权重固定为 1
其他权重表示相对 expected_value 的重要程度
temperature 单独控制决策确定性
```

### 14.4 参数范围动态修正

训练若干代后检查优秀 AI 的参数分布。

如果连续 10-20 代中，超过 60% 的精英参数落在某参数最外侧 5% 范围，则扩大该侧边界 25%-50%。

如果某参数在优秀 AI 中仍然近似均匀分布，并且移除后胜率不变，则考虑删除该参数。

不要因为单代冠军触边就立即修改范围。

### 14.5 公共随机种子和座次轮换

比较候选 AI 时，应使用相同的一批随机种子，并轮换四个座次。

建议评估方式：

```text
for seed in evaluation_seeds:
  for seat in [1,2,3,4]:
    candidate_ai 坐 seat
    baseline_ai 坐其他位置
```

这样可以减少“只是运气更好”或“座位更好”造成的评分噪声。

### 14.6 神经网络训练

后续神经网络不直接输出这些人工参数，而是：

- 输入状态特征。
- 输出固定动作空间 logits。
- 使用合法动作 mask。
- 训练时仍可把上面的参数和特征作为 feature engineering 参考。

推荐先训练可解释参数 AI，再做神经网络。

## 15. 第二阶段可增加参数

第一版先训练 18 维参数。规则稳定、GreedyAI 可稳定强于 RandomAI 后，再考虑增加以下参数。

### 15.1 位置类型偏置

用于修正评估函数对不同位置的系统性误差。

```text
bias_harbor
bias_shipyard
bias_pirate
bias_insurance
bias_navigator
bias_market
```

值域统一：

```text
-0.75 到 0.75
```

货船位置可作为基准：

```text
bias_boat = 0
```

或者每次变异后将所有偏置减去平均值，保证：

```text
所有 action bias 之和 = 0
```

否则所有偏置同时增加没有行为意义。

### 15.2 隐藏信息参数

当前电子版状态大多公开，第一版不需要隐藏信息参数。

如果后续货物股份或玩家资产改为隐藏，可加入：

| 参数 | 值域 | 初始值 | 含义 |
| --- | ---: | ---: | --- |
| `belief_update_rate` | 0.05-1.0 | 0.3 | 根据对手行为更新推测的速度 |
| `belief_strength` | 0-1.5 | 0.5 | 对推测出的对手资产有多信任 |

加入这些参数后，完整版本约 25 维。

## 16. 第一阶段实现建议

下一步先实现 `GreedyAI`：

1. 定义 `AIWeights` 结构，包含 18 个第一版参数。
2. 定义 `AIGenome`，内部统一存 `[0,1]`。
3. 实现 `DecodeGenome(AIGenome) -> AIWeights`。
4. 定义 `ActionFeatures`，包含 `expected_value`、`cash_safety`、`future_flexibility`、`own_asset_gain`、`opponent_denial`、`leader_denial`、`return_variance`、`upside`、`tail_loss`、`uncertainty`。
5. 为每个合法动作计算归一化特征。
6. 使用本文件第 1 节公式打分。
7. 使用 softmax temperature 选择动作，而不是永远取最高分。
8. 和 RandomAI 批量对战，统计平均排名、胜率、财富差。

第一阶段不需要神经网络。先证明 18 参数 AI 可以稳定跑完整局，并且显著强于 RandomAI。

## 17. 竞拍优化补充（当前实现）

当前 EvolvableAI 已从 18 维参数扩展为 27 维参数。旧 genome 或旧 weights 文件仍可加载：缺失的新增参数会使用中性默认值，已删除的旧跳价压力参数会在读取旧 genome 时自动跳过。

### 17.1 前任港务长开局防守规则

如果当前玩家是上一任港务长、当前竞拍还没有任何人出价，并且该玩家至少能支付最低叫价，则 AI 会过滤掉 `PassBid`。

原因是：一旦 pass，本轮竞拍就永久退出；而叫价 1 元通常成本极低，却能保留竞拍主动权。这个行为不交给训练权重决定，作为确定性规则处理。

### 17.2 心理价与最小加价

竞拍按英式竞拍处理。AI 先估算自己愿意为港务长支付的最高心理价 `reservationPrice`，但实际出价不直接跳到心理价，而是只出最低合法价：

```text
if minBid <= reservationPrice:
  bidAmount = minBid
else:
  pass
```

原因是：如果当前只需要加 1，直接跳到心理价会放弃“未来还能继续加价”的期权；一旦其他玩家放弃，自己会以过高价格成交。当前版本不建模“跳价威慑”，因此不允许大跨度加价。

### 17.3 新增竞拍参数

| 参数 | 解码范围 | 缺省值 | 含义 |
| --- | ---: | ---: | --- |
| `auction_share_value_multiplier` | 0.2-2.0 | 1.0/中性 | 港务长“优先买股份”价值倍率 |
| `auction_control_value_multiplier` | 0-2.0 | 1.0/中性 | 港务长“选货和控船”价值倍率 |
| `auction_first_move_value_multiplier` | 0-2.0 | 1.0/中性 | 港务长“放置先手”价值倍率 |
| `auction_price_sensitivity` | 0.4-1.6 | 1.0 | 对出价成本的惩罚倍率 |

新的港务长估值不再只看一张股份，而是：

```text
harborMasterValue =
    shareValue * auction_share_value_multiplier
  + controlValue * auction_control_value_multiplier
  + firstMoveValue * auction_first_move_value_multiplier
```

竞价动作的期望值：

```text
bidExpectedValue =
    harborMasterValue
  - bidAmount * auction_price_sensitivity
```

### 17.4 重点参数变异

训练命令支持选择一部分参数作为重点变异对象：

```text
-mutationFocus auction_share_value_multiplier,auction_price_sensitivity
```

也可以使用 0 基因编号：

```text
-mutationFocus 18,21,22
```

重点参数在训练初期使用更高的随机重置概率和更大的 sigma 倍率，随后随代数线性衰减，生成最终评估代时回归普通变异：

```text
focus_reset_probability =
    base_reset_probability
  + (focus_start_reset_probability - base_reset_probability) * decay

focus_sigma_multiplier =
    1 + (focus_start_sigma_multiplier - 1) * decay
```

其中：

```text
decay = 1 - (generation - 1) / (generations - 2)
```

默认值：

```text
mutationResetProbability = 0.12
mutationFocusResetProbability = 0.28
mutationFocusSigmaMultiplier = 2.0
```

可以用以下命令查看参数编号和名称：

```text
go run ./cmd/train-ai -listGenes
```
