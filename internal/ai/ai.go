package ai

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"

	"manila/internal/model"
	"manila/internal/rules"
)

const GeneCount = 27
const piratePortWeight = 0.5
const pirateBoardingWeight = 0.35
const totalSharesPerGoods = 5
const initialHiddenSharesPerPlayer = 2

var GeneNames = []string{
	"w_cash",
	"w_flexibility",
	"w_own_asset",
	"w_opponent_denial",
	"w_leader_denial",
	"w_variance",
	"w_tail_loss",
	"w_upside",
	"w_uncertainty",
	"temperature",
	"bid_value_multiplier",
	"bid_cash_cap",
	"reserve_cash_ratio",
	"bid_block_weight",
	"leading_risk_shift",
	"trailing_risk_shift",
	"late_game_risk_shift",
	"navigator_value_multiplier",
	"auction_share_value_multiplier",
	"auction_control_value_multiplier",
	"auction_first_move_value_multiplier",
	"auction_price_sensitivity",
	"cargo_own_share_weight",
	"cargo_leading_high_value_weight",
	"cargo_trailing_low_value_weight",
	"cargo_trailing_endgame_penalty",
	"cargo_visible_leader_deny_weight",
}

var moveProbabilityTables = buildMoveProbabilityTables(3)

type Genome struct {
	Genes []float64 `json:"genes"`
}

type MutationProfile struct {
	FocusGenes            map[int]bool
	BaseResetProbability  float64
	FocusResetProbability float64
	FocusSigmaMultiplier  float64
}

type Weights struct {
	WCash                   float64 `json:"w_cash"`
	WFlexibility            float64 `json:"w_flexibility"`
	WOwnAsset               float64 `json:"w_own_asset"`
	WOpponentDenial         float64 `json:"w_opponent_denial"`
	WLeaderDenial           float64 `json:"w_leader_denial"`
	WVariance               float64 `json:"w_variance"`
	WTailLoss               float64 `json:"w_tail_loss"`
	WUpside                 float64 `json:"w_upside"`
	WUncertainty            float64 `json:"w_uncertainty"`
	Temperature             float64 `json:"temperature"`
	BidValueMultiplier      float64 `json:"bid_value_multiplier"`
	BidCashCap              float64 `json:"bid_cash_cap"`
	ReserveCashRatio        float64 `json:"reserve_cash_ratio"`
	BidBlockWeight          float64 `json:"bid_block_weight"`
	LeadingRiskShift        float64 `json:"leading_risk_shift"`
	TrailingRiskShift       float64 `json:"trailing_risk_shift"`
	LateGameRiskShift       float64 `json:"late_game_risk_shift"`
	NavigatorValue          float64 `json:"navigator_value_multiplier"`
	AuctionShareValue       float64 `json:"auction_share_value_multiplier"`
	AuctionControlValue     float64 `json:"auction_control_value_multiplier"`
	AuctionFirstMoveValue   float64 `json:"auction_first_move_value_multiplier"`
	AuctionPriceSensitivity float64 `json:"auction_price_sensitivity"`
	CargoOwnShareWeight     float64 `json:"cargo_own_share_weight"`
	CargoLeadingHighValue   float64 `json:"cargo_leading_high_value_weight"`
	CargoTrailingLowValue   float64 `json:"cargo_trailing_low_value_weight"`
	CargoTrailingEndPenalty float64 `json:"cargo_trailing_endgame_penalty"`
	CargoLeaderDeny         float64 `json:"cargo_visible_leader_deny_weight"`
}

type ActionFeatures struct {
	ExpectedValue  float64 `json:"expectedValue"`
	CashSafety     float64 `json:"cashSafety"`
	Flexibility    float64 `json:"flexibility"`
	OwnAssetGain   float64 `json:"ownAssetGain"`
	OpponentDenial float64 `json:"opponentDenial"`
	LeaderDenial   float64 `json:"leaderDenial"`
	Variance       float64 `json:"variance"`
	TailLoss       float64 `json:"tailLoss"`
	Upside         float64 `json:"upside"`
	Uncertainty    float64 `json:"uncertainty"`
}

type Agent interface {
	ChooseAction(engine *rules.Engine, g *model.Game, playerID int) (model.Action, error)
}

type RandomAgent struct {
	Rand *rand.Rand
}

type EvolvableAgent struct {
	Weights Weights
	Rand    *rand.Rand
}

func NewRandomAgent(seed int64) *RandomAgent {
	return &RandomAgent{Rand: rand.New(rand.NewSource(seed))}
}

func NewEvolvableAgent(genome Genome, seed int64) *EvolvableAgent {
	return &EvolvableAgent{
		Weights: DecodeGenome(genome),
		Rand:    rand.New(rand.NewSource(seed)),
	}
}

func NewWeightedAgent(weights Weights, seed int64) *EvolvableAgent {
	return &EvolvableAgent{
		Weights: normalizeWeights(weights),
		Rand:    rand.New(rand.NewSource(seed)),
	}
}

func normalizeWeights(weights Weights) Weights {
	if weights.NavigatorValue == 0 {
		weights.NavigatorValue = 1.0
	}
	if weights.AuctionShareValue == 0 &&
		weights.AuctionControlValue == 0 &&
		weights.AuctionFirstMoveValue == 0 &&
		weights.AuctionPriceSensitivity == 0 {
		weights.AuctionShareValue = 1.0
		weights.AuctionControlValue = 1.0
		weights.AuctionFirstMoveValue = 1.0
		weights.AuctionPriceSensitivity = 1.0
	}
	if weights.AuctionPriceSensitivity == 0 {
		weights.AuctionPriceSensitivity = 1.0
	}
	if weights.CargoOwnShareWeight == 0 &&
		weights.CargoLeadingHighValue == 0 &&
		weights.CargoTrailingLowValue == 0 &&
		weights.CargoTrailingEndPenalty == 0 &&
		weights.CargoLeaderDeny == 0 {
		weights.CargoOwnShareWeight = 2.25
		weights.CargoLeadingHighValue = 1.0
		weights.CargoTrailingLowValue = 1.0
		weights.CargoTrailingEndPenalty = 1.5
		weights.CargoLeaderDeny = 1.0
	}
	return weights
}

func NewGreedyAgent(seed int64) *EvolvableAgent {
	return NewWeightedAgent(GreedyWeights(), seed)
}

func GreedyWeights() Weights {
	return Weights{
		WCash:                   0.6,
		WFlexibility:            0.4,
		WOwnAsset:               1.0,
		WOpponentDenial:         0.35,
		WLeaderDenial:           0.25,
		WVariance:               0.0,
		WTailLoss:               -0.6,
		WUpside:                 0.2,
		WUncertainty:            -0.3,
		Temperature:             0.12,
		BidValueMultiplier:      0.95,
		BidCashCap:              0.45,
		ReserveCashRatio:        0.15,
		BidBlockWeight:          0.15,
		LeadingRiskShift:        -0.35,
		TrailingRiskShift:       0.45,
		LateGameRiskShift:       0.1,
		NavigatorValue:          1.0,
		AuctionShareValue:       1.0,
		AuctionControlValue:     1.0,
		AuctionFirstMoveValue:   1.0,
		AuctionPriceSensitivity: 1.0,
		CargoOwnShareWeight:     2.25,
		CargoLeadingHighValue:   1.0,
		CargoTrailingLowValue:   1.0,
		CargoTrailingEndPenalty: 1.5,
		CargoLeaderDeny:         1.0,
	}
}

func RandomGenome(r *rand.Rand) Genome {
	genes := make([]float64, GeneCount)
	for i := range genes {
		genes[i] = r.Float64()
	}
	return Genome{Genes: genes}
}

func NeutralGenome() Genome {
	genes := make([]float64, GeneCount)
	for i := range genes {
		genes[i] = 0.5
	}
	return Genome{Genes: genes}
}

func DecodeGenome(genome Genome) Weights {
	normalizedGenes := normalizeGenomeGenes(genome.Genes)
	genes := make([]float64, GeneCount)
	copy(genes, normalizedGenes)
	for i := range genes {
		genes[i] = clamp01(genes[i])
	}
	for i := len(normalizedGenes); i < GeneCount; i++ {
		genes[i] = 0.5
	}
	return Weights{
		WCash:                   linear(genes[0], -1.5, 1.5),
		WFlexibility:            linear(genes[1], -1.0, 1.5),
		WOwnAsset:               linear(genes[2], -1.0, 2.0),
		WOpponentDenial:         linear(genes[3], -1.0, 1.5),
		WLeaderDenial:           linear(genes[4], -1.0, 2.0),
		WVariance:               linear(genes[5], -1.5, 1.5),
		WTailLoss:               linear(genes[6], -2.0, 0.5),
		WUpside:                 linear(genes[7], -0.5, 2.0),
		WUncertainty:            linear(genes[8], -2.0, 0.5),
		Temperature:             logLinear(genes[9], 0.01, 1.0),
		BidValueMultiplier:      linear(genes[10], 0.0, 2.0),
		BidCashCap:              linear(genes[11], 0.1, 1.0),
		ReserveCashRatio:        linear(genes[12], 0.0, 0.8),
		BidBlockWeight:          linear(genes[13], 0.0, 2.0),
		LeadingRiskShift:        linear(genes[14], -1.0, 1.0),
		TrailingRiskShift:       linear(genes[15], -1.0, 1.0),
		LateGameRiskShift:       linear(genes[16], -1.0, 1.0),
		NavigatorValue:          linear(genes[17], 0.2, 1.8),
		AuctionShareValue:       linear(genes[18], 0.2, 2.0),
		AuctionControlValue:     linear(genes[19], 0.0, 2.0),
		AuctionFirstMoveValue:   linear(genes[20], 0.0, 2.0),
		AuctionPriceSensitivity: linear(genes[21], 0.4, 1.6),
		CargoOwnShareWeight:     linear(genes[22], 0.5, 4.0),
		CargoLeadingHighValue:   linear(genes[23], 0.0, 2.0),
		CargoTrailingLowValue:   linear(genes[24], -1.0, 2.0),
		CargoTrailingEndPenalty: linear(genes[25], 0.0, 3.0),
		CargoLeaderDeny:         linear(genes[26], 0.0, 2.0),
	}
}

func normalizeGenomeGenes(genes []float64) []float64 {
	if len(genes) == 23 || len(genes) > GeneCount {
		out := make([]float64, 0, len(genes)-1)
		out = append(out, genes[:21]...)
		out = append(out, genes[22:]...)
		return out
	}
	return genes
}

func MutateGenome(parent Genome, r *rand.Rand, sigma float64) Genome {
	return MutateGenomeWithProfile(parent, r, sigma, MutationProfile{})
}

func MutateGenomeWithProfile(parent Genome, r *rand.Rand, sigma float64, profile MutationProfile) Genome {
	if sigma <= 0 {
		sigma = 0.12
	}
	baseResetProbability := profile.BaseResetProbability
	if baseResetProbability <= 0 {
		baseResetProbability = 0.12
	}
	focusResetProbability := profile.FocusResetProbability
	if focusResetProbability <= 0 {
		focusResetProbability = baseResetProbability
	}
	focusSigmaMultiplier := profile.FocusSigmaMultiplier
	if focusSigmaMultiplier <= 0 {
		focusSigmaMultiplier = 1
	}
	parentGenes := normalizeGenomeGenes(parent.Genes)
	child := Genome{Genes: make([]float64, GeneCount)}
	for i := 0; i < GeneCount; i++ {
		v := 0.5
		if i < len(parentGenes) {
			v = parentGenes[i]
		}
		resetProbability := baseResetProbability
		geneSigma := sigma
		if profile.FocusGenes[i] {
			resetProbability = focusResetProbability
			geneSigma *= focusSigmaMultiplier
		}
		if r.Float64() < resetProbability {
			v = r.Float64()
		} else {
			v += r.NormFloat64() * geneSigma
		}
		child.Genes[i] = clamp01(v)
	}
	return child
}

func GeneIndex(name string) (int, bool) {
	for i, geneName := range GeneNames {
		if name == geneName {
			return i, true
		}
	}
	return 0, false
}

func (a *RandomAgent) ChooseAction(engine *rules.Engine, g *model.Game, playerID int) (model.Action, error) {
	acts, err := engine.LegalActions(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	if len(acts) == 0 {
		return model.Action{}, fmt.Errorf("no legal actions for player %d phase %s", playerID, g.Phase)
	}
	r := a.Rand
	if r == nil {
		r = rand.New(rand.NewSource(g.Seed + int64(g.EventSeq+1)*104729 + int64(playerID)*97))
	}
	return CompleteRandomAction(g, playerID, acts[r.Intn(len(acts))], r)
}

func (a *EvolvableAgent) ChooseAction(engine *rules.Engine, g *model.Game, playerID int) (model.Action, error) {
	acts, err := engine.LegalActions(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	if len(acts) == 0 {
		return model.Action{}, fmt.Errorf("no legal actions for player %d phase %s", playerID, g.Phase)
	}
	if g.Phase == model.PhaseAuction {
		return a.chooseAuctionAction(g, playerID, acts)
	}
	candidates := make([]model.Action, 0, len(acts))
	scores := make([]float64, 0, len(acts))
	for _, legal := range acts {
		for _, action := range a.completeActions(g, playerID, legal) {
			features := a.Features(g, playerID, legal, action)
			candidates = append(candidates, action)
			scores = append(scores, a.ScoreFeatures(features))
		}
	}
	if len(candidates) == 0 {
		return model.Action{}, fmt.Errorf("no complete actions for player %d phase %s", playerID, g.Phase)
	}
	idx := SoftmaxChoice(scores, math.Max(a.Weights.Temperature, 0.01), a.Rand)
	return candidates[idx], nil
}

func (a *EvolvableAgent) chooseAuctionAction(g *model.Game, playerID int, acts []model.LegalAction) (model.Action, error) {
	var pass *model.LegalAction
	for _, legal := range acts {
		if legal.Type == model.ActionPassBid {
			copyLegal := legal
			pass = &copyLegal
			continue
		}
		if legal.Type != model.ActionBid {
			continue
		}
		payload := copyPayload(legal.Payload)
		minBid := intFromPayload(payload["minBid"], 1)
		maxBid := intFromPayload(payload["maxBid"], minBid)
		if maxBid < minBid {
			return model.Action{}, fmt.Errorf("invalid bid range")
		}
		if a.bidReservationPrice(g, playerID, minBid, maxBid) >= minBid {
			payload["amount"] = minBid
			return model.Action{PlayerID: playerID, Type: model.ActionBid, Payload: payload}, nil
		}
	}
	if pass != nil {
		return model.Action{PlayerID: playerID, Type: model.ActionPassBid, Payload: copyPayload(pass.Payload)}, nil
	}
	return model.Action{}, fmt.Errorf("no auction action for player %d", playerID)
}

func (a *EvolvableAgent) ScoreFeatures(f ActionFeatures) float64 {
	w := a.Weights
	return f.ExpectedValue +
		w.WCash*f.CashSafety +
		w.WFlexibility*f.Flexibility +
		w.WOwnAsset*f.OwnAssetGain +
		w.WOpponentDenial*f.OpponentDenial +
		w.WLeaderDenial*f.LeaderDenial +
		w.WVariance*f.Variance +
		w.WTailLoss*f.TailLoss +
		w.WUpside*f.Upside +
		w.WUncertainty*f.Uncertainty
}

func (a *EvolvableAgent) completeAction(g *model.Game, playerID int, legal model.LegalAction) model.Action {
	actions := a.completeActions(g, playerID, legal)
	if len(actions) > 0 {
		return actions[0]
	}
	return model.Action{PlayerID: playerID, Type: legal.Type, Payload: copyPayload(legal.Payload)}
}

func (a *EvolvableAgent) completeActions(g *model.Game, playerID int, legal model.LegalAction) []model.Action {
	payload := copyPayload(legal.Payload)
	switch legal.Type {
	case model.ActionBid:
		minBid := intFromPayload(payload["minBid"], 1)
		maxBid := intFromPayload(payload["maxBid"], minBid)
		amounts := a.bidCandidateAmounts(g, playerID, minBid, maxBid)
		actions := make([]model.Action, 0, len(amounts))
		for _, amount := range amounts {
			bidPayload := copyPayload(payload)
			bidPayload["amount"] = amount
			actions = append(actions, model.Action{PlayerID: playerID, Type: legal.Type, Payload: bidPayload})
		}
		return actions
	case model.ActionSelectGoods:
		payload["goodsIds"] = a.selectGoods(g, playerID)
	case model.ActionSetShipStarts:
		payload["starts"] = a.shipStarts(g, playerID)
	case model.ActionNavigatorMove:
		payload["moves"] = a.navigatorMoves(g, playerID)
	}
	return []model.Action{{PlayerID: playerID, Type: legal.Type, Payload: payload}}
}

func (a *EvolvableAgent) bidCandidateAmounts(g *model.Game, playerID int, minBid int, maxBid int) []int {
	if maxBid < minBid {
		return nil
	}
	reservation := a.bidReservationPrice(g, playerID, minBid, maxBid)
	if reservation < minBid {
		return nil
	}
	return []int{minBid}
}

func (a *EvolvableAgent) bidReservationPrice(g *model.Game, playerID int, minBid int, maxBid int) int {
	harborValue := a.harborMasterValue(g, playerID)
	blockValue := a.leaderAssetPressure(g, playerID) * 6 * a.Weights.BidBlockWeight
	target := int(math.Round(harborValue*a.Weights.BidValueMultiplier + blockValue))
	upper := a.bidUpperBound(g, playerID, minBid, maxBid)
	if target > upper {
		target = upper
	}
	return target
}

func (a *EvolvableAgent) bidUpperBound(g *model.Game, playerID int, minBid int, maxBid int) int {
	p := g.Players[playerID]
	payable := maxPayable(p)
	cashCap := int(math.Round(float64(payable) * a.Weights.BidCashCap))
	reserve := int(math.Round(float64(p.Cash) * a.Weights.ReserveCashRatio))
	reserveCap := payable - reserve
	upper := cashCap
	if upper > reserveCap {
		upper = reserveCap
	}
	if upper < minBid {
		upper = minBid
	}
	return clampInt(upper, minBid, maxBid)
}

func (a *EvolvableAgent) Features(g *model.Game, playerID int, legal model.LegalAction, action model.Action) ActionFeatures {
	cost := legal.Cost
	if action.Type == model.ActionBid {
		cost = intFromPayload(action.Payload["amount"], cost)
	}
	f := ActionFeatures{
		CashSafety:  cashSafety(g.Players[playerID], cost),
		Flexibility: flexibilityAfterCost(g.Players[playerID], cost),
		Uncertainty: -0.05,
	}
	switch action.Type {
	case model.ActionPassBid, model.ActionSkipBuyShare, model.ActionPirateSkipBoard:
		f.ExpectedValue = 0
		f.CashSafety += 0.1
	case model.ActionBid:
		pricePenalty := float64(cost) * a.Weights.AuctionPriceSensitivity
		f.ExpectedValue = (a.harborMasterValue(g, playerID) - pricePenalty) / 12
		f.Variance = 0.15
		f.Upside = 0.1
		f.TailLoss = -float64(cost) / 24
	case model.ActionBuyShare:
		gid := model.GoodsID(intFromPayload(action.Payload["goodsId"], 0))
		gd := g.Goods[gid]
		price := gd.MarketValue()
		if price < 5 {
			price = 5
		}
		f.ExpectedValue = (float64(gd.MarketValue()+5) - float64(price)) / 12
		f.OwnAssetGain = 0.25 + float64(gd.MarketValueIndex)/8
		f.Upside = float64(model.ShipPayout(gid)) / 72
	case model.ActionSelectGoods:
		ids := interfaceSlice(action.Payload["goodsIds"])
		for _, raw := range ids {
			gid := model.GoodsID(intFromPayload(raw, 0))
			f.ExpectedValue += float64(model.ShipPayout(gid)) / 72
			f.OwnAssetGain += float64(g.Players[playerID].Shares[gid]) * 0.25
		}
	case model.ActionSetShipStarts:
		starts := parseShipStarts(action.Payload["starts"])
		f.ExpectedValue = a.shipStartPlanScore(g, playerID, starts) / 12
		f.OwnAssetGain = a.selectedOwnSharePressure(g, playerID) * 0.2
		f.Flexibility += 0.1
	case model.ActionPlaceAccomplice:
		a.scorePlacementFeatures(g, playerID, action, cost, &f)
	case model.ActionPirateBoard:
		gid := model.GoodsID(intFromPayload(action.Payload["shipId"], 0))
		ship := g.Ships[gid]
		shareCount := len(ship.Occupants) + 1
		f.ExpectedValue = shipArrivalProbability(g, gid, nil) * float64(model.ShipPayout(gid)/shareCount) / 12
		f.Upside = float64(model.ShipPayout(gid)) / 72
		f.Variance = 0.4
	case model.ActionNavigatorMove:
		moves := parseNavigatorMoves(action.Payload["moves"])
		delta := a.navigatorMoveScore(g, playerID, moves)
		f.ExpectedValue = delta / 12
		f.OwnAssetGain = math.Max(0, delta) / 24
		f.OpponentDenial = math.Max(0, -delta) / 30
		f.Flexibility += clamp(math.Abs(delta)/24, 0, 0.4)
		f.Uncertainty = -0.08
	case model.ActionPirateChooseDestination:
		dest, _ := action.Payload["destination"].(string)
		gid := model.GoodsID(intFromPayload(action.Payload["shipId"], 0))
		if dest == "port" {
			f.ExpectedValue = float64(model.ShipPayout(gid)) / 24
			f.OwnAssetGain = float64(g.Players[playerID].Shares[gid]) * 0.25
		} else {
			f.ExpectedValue = 0.2
			f.OpponentDenial = opponentSharePressure(g, playerID, gid) * 0.25
			f.LeaderDenial = leaderSharePressure(g, playerID, gid) * 0.3
		}
		f.Variance = 0.2
	}
	a.applyRiskContext(g, playerID, &f)
	return f
}

func (a *EvolvableAgent) scorePlacementFeatures(g *model.Game, playerID int, action model.Action, cost int, f *ActionFeatures) {
	pos, _ := action.Payload["positionType"].(string)
	target := action.Payload["targetId"]
	f.ExpectedValue = -float64(cost) / 12
	switch pos {
	case "ship":
		gid := model.GoodsID(intFromPayload(target, 0))
		ship := g.Ships[gid]
		if ship == nil {
			return
		}
		shareCount := len(ship.Occupants) + 1
		prob := shipArrivalProbability(g, gid, nil)
		payout := float64(model.ShipPayout(gid)) / float64(shareCount)
		f.ExpectedValue += prob * payout / 12
		f.OwnAssetGain = float64(g.Players[playerID].Shares[gid]) * 0.2
		f.OpponentDenial = -opponentSharePressure(g, playerID, gid) * 0.05
		f.Variance = 0.35
		f.Upside = payout / 24
		f.TailLoss = -float64(cost) / 12
	case "port":
		slotID := fmt.Sprint(target)
		slot := g.Board.Ports[slotID]
		a.scorePortDockPlacementFeatures(slotID, true, slot, cost, f, g)
	case "dock":
		slotID := fmt.Sprint(target)
		slot := g.Board.Docks[slotID]
		a.scorePortDockPlacementFeatures(slotID, false, slot, cost, f, g)
	case "pirate":
		a.scorePiratePlacementFeatures(g, playerID, cost, f)
		f.TailLoss = -float64(cost) / 12
	case "navigatorSmall":
		potential := a.bestNavigatorMoveScore(g, playerID, "small")
		navValue := a.Weights.NavigatorValue
		f.ExpectedValue += navValue * (0.08 + math.Max(0, potential)/12)
		f.Flexibility += navValue * 0.3
		f.Uncertainty = -navValue * 0.1
	case "navigatorBig":
		potential := a.bestNavigatorMoveScore(g, playerID, "big")
		navValue := a.Weights.NavigatorValue
		f.ExpectedValue += navValue * (0.12 + math.Max(0, potential)/12)
		f.Flexibility += navValue * 0.45
		f.Uncertainty = -navValue * 0.12
	case "insurance":
		a.scoreInsurancePlacementFeatures(g, f)
	}
}

func (a *EvolvableAgent) scorePortDockPlacementFeatures(slotID string, port bool, slot *model.BoardSlot, cost int, f *ActionFeatures, g *model.Game) {
	if slot == nil {
		return
	}
	prob := slotHitProbability(slotID, port, g)
	reward := float64(slot.Reward)
	net := prob*reward - float64(cost)
	f.ExpectedValue += prob * reward / 12
	if net > 0 {
		normalizedNet := clamp(net/12, 0, 0.5)
		f.CashSafety += normalizedNet
		f.Flexibility += clamp(net/18, 0, 0.35)
		f.TailLoss = 0
	}
	f.Variance = clamp(prob*(1-prob)*reward/12, 0, 0.5)
	f.Upside = clamp(math.Max(0, reward-float64(cost))/24, 0, 1)
	f.Uncertainty -= clamp(math.Abs(prob-0.5)*0.12, 0, 0.06)
}

func (a *EvolvableAgent) scoreInsurancePlacementFeatures(g *model.Game, f *ActionFeatures) {
	liability := 0.0
	for _, slotID := range []string{"A", "B", "C"} {
		slot := g.Board.Docks[slotID]
		if slot == nil || slot.Occupant == nil {
			continue
		}
		liability += slotHitProbability(slotID, false, g) * float64(slot.Reward)
	}
	net := 10.0 - liability
	f.ExpectedValue += net / 12
	f.CashSafety += 0.6
	f.Variance = clamp(liability/18, -0.4, 0.8)
	f.TailLoss = -clamp(liability/12, 0, 1)
	f.Upside = clamp(10.0/12.0, 0, 1)
	f.Uncertainty -= clamp(liability/30, 0, 0.4)
}

func (a *EvolvableAgent) scorePiratePlacementFeatures(g *model.Game, playerID int, cost int, f *ActionFeatures) {
	piratesAfter := len(g.Board.Pirates) + 1
	if piratesAfter < 1 {
		piratesAfter = 1
	}
	lootEV := 0.0
	boardingEV := 0.0
	bestLoot := 0.0
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship == nil || ship.Status != model.ShipSailing {
			continue
		}
		lootProb := shipPirateLootProbability(g, gid)
		boardingProb := shipPirateBoardingProbability(g, gid)
		payout := float64(model.ShipPayout(gid))
		lootValue := lootProb * payout / float64(piratesAfter)
		lootEV += lootValue
		if lootValue > bestLoot {
			bestLoot = lootValue
		}
		if g.Round.MovementStep < 2 && ship.NextBoardingIndex < len(model.BoardingCosts(gid)) {
			crewShare := payout / float64(len(ship.Occupants)+1)
			boardingEV += boardingProb * crewShare * pirateBoardingWeight
		}
	}
	controlValue := 0.0
	if len(g.Board.Pirates) == 0 {
		controlValue = 0.35
	} else {
		controlValue = 0.12
	}
	f.ExpectedValue += (lootEV + boardingEV) / 12
	f.OwnAssetGain += a.selectedOwnSharePressure(g, playerID) * 0.03
	f.LeaderDenial += a.pirateLeaderDenial(g, playerID) * 0.08
	f.Variance = clamp(bestLoot/18, 0.05, 1)
	f.Upside = clamp(bestLoot/12+controlValue, 0, 1)
	f.Uncertainty -= 0.12
	if g.Round.MovementStep == 0 {
		f.Uncertainty -= 0.10
	}
}

func (a *EvolvableAgent) applyRiskContext(g *model.Game, playerID int, f *ActionFeatures) {
	rank := estimatedRank(g, playerID)
	if rank == 1 {
		f.Variance += a.Weights.LeadingRiskShift * 0.25
		f.TailLoss += -math.Abs(a.Weights.LeadingRiskShift) * 0.1
	} else if rank >= 3 {
		f.Variance += a.Weights.TrailingRiskShift * 0.25
		f.Upside += math.Max(0, a.Weights.TrailingRiskShift) * 0.15
	}
	if maxMarketIndex(g) >= 3 {
		f.Variance += a.Weights.LateGameRiskShift * 0.2
	}
}

func CompleteRandomAction(g *model.Game, playerID int, legal model.LegalAction, r *rand.Rand) (model.Action, error) {
	payload := copyPayload(legal.Payload)
	switch legal.Type {
	case model.ActionBid:
		minBid := intFromPayload(payload["minBid"], 1)
		maxBid := intFromPayload(payload["maxBid"], minBid)
		if maxBid < minBid {
			return model.Action{}, fmt.Errorf("invalid bid range")
		}
		payload["amount"] = minBid + r.Intn(maxBid-minBid+1)
	case model.ActionSelectGoods:
		goods := append([]model.GoodsID{}, model.GoodsOrder...)
		r.Shuffle(len(goods), func(i, j int) { goods[i], goods[j] = goods[j], goods[i] })
		payload["goodsIds"] = []interface{}{int(goods[0]), int(goods[1]), int(goods[2])}
	case model.ActionSetShipStarts:
		payload["starts"] = randomShipStarts(g, r)
	case model.ActionNavigatorMove:
		payload["moves"] = []interface{}{}
	}
	return model.Action{PlayerID: playerID, Type: legal.Type, Payload: payload}, nil
}

func SoftmaxChoice(scores []float64, temperature float64, r *rand.Rand) int {
	if len(scores) == 0 {
		return 0
	}
	if r == nil {
		r = rand.New(rand.NewSource(1))
	}
	if temperature <= 0 {
		temperature = 0.01
	}
	best := scores[0]
	for _, score := range scores[1:] {
		if score > best {
			best = score
		}
	}
	total := 0.0
	weights := make([]float64, len(scores))
	for i, score := range scores {
		w := math.Exp((score - best) / temperature)
		weights[i] = w
		total += w
	}
	roll := r.Float64() * total
	for i, w := range weights {
		roll -= w
		if roll <= 0 {
			return i
		}
	}
	return len(scores) - 1
}

func (a *EvolvableAgent) selectGoods(g *model.Game, playerID int) []interface{} {
	bestScore := math.Inf(-1)
	best := []model.GoodsID{}
	for _, combo := range cargoCombinations() {
		score := a.cargoSelectionScore(g, playerID, combo)
		if score > bestScore || (math.Abs(score-bestScore) < 0.000001 && goodsListLess(combo, best)) {
			bestScore = score
			best = combo
		}
	}
	if len(best) == 0 {
		best = append([]model.GoodsID{}, model.GoodsOrder[:3]...)
	}
	out := make([]interface{}, 0, len(best))
	for _, gid := range best {
		out = append(out, int(gid))
	}
	return out
}

func cargoCombinations() [][]model.GoodsID {
	combos := make([][]model.GoodsID, 0, len(model.GoodsOrder))
	for excludedIndex := range model.GoodsOrder {
		combo := []model.GoodsID{}
		for i, gid := range model.GoodsOrder {
			if i != excludedIndex {
				combo = append(combo, gid)
			}
		}
		combos = append(combos, combo)
	}
	return combos
}

func (a *EvolvableAgent) cargoSelectionScore(g *model.Game, playerID int, goods []model.GoodsID) float64 {
	leading, trailing := visibleLeadTrailRatios(g, playerID)
	score := 0.0
	for _, gid := range goods {
		gd := g.Goods[gid]
		if gd == nil {
			continue
		}
		ownShares := float64(g.Players[playerID].Shares[gid])
		leaderShares := leaderSharePressure(g, playerID, gid)
		marketGain := marketAdvanceValue(g, gid)
		marketIndex := float64(gd.MarketValueIndex)
		highValue := marketIndex / float64(len(model.MarketValues)-1)
		lowValue := 1 - highValue
		endgameRisk := 0.0
		if gd.MarketValueIndex >= len(model.MarketValues)-2 {
			endgameRisk = 1.0
		}
		score += float64(model.ShipPayout(gid)) / 72
		score += ownShares * (1 + marketGain/10) * a.Weights.CargoOwnShareWeight
		score += leading * highValue * a.Weights.CargoLeadingHighValue
		score += trailing * lowValue * a.Weights.CargoTrailingLowValue
		score -= trailing * endgameRisk * a.Weights.CargoTrailingEndPenalty
		score -= leaderShares * (1 + marketGain/10) * a.Weights.CargoLeaderDeny
	}
	return score
}

func goodsListLess(a, b []model.GoodsID) bool {
	if len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func (a *EvolvableAgent) shipStarts(g *model.Game, playerID int) map[string]interface{} {
	bestScore := math.Inf(-1)
	var best map[model.GoodsID]int
	for _, starts := range validShipStartPlans(g.Round.SelectedGoods) {
		score := a.shipStartPlanScore(g, playerID, starts)
		if score > bestScore || (math.Abs(score-bestScore) < 0.000001 && shipStartPlanLess(starts, best, g.Round.SelectedGoods)) {
			bestScore = score
			best = starts
		}
	}
	starts := map[string]interface{}{}
	if best == nil {
		for _, gid := range g.Round.SelectedGoods {
			starts[fmt.Sprint(int(gid))] = 3
		}
		return starts
	}
	for _, gid := range g.Round.SelectedGoods {
		starts[fmt.Sprint(int(gid))] = best[gid]
	}
	return starts
}

func validShipStartPlans(goods []model.GoodsID) []map[model.GoodsID]int {
	plans := []map[model.GoodsID]int{}
	if len(goods) != 3 {
		return plans
	}
	for a := 0; a <= 5; a++ {
		for b := 0; b <= 5; b++ {
			c := 9 - a - b
			if c < 0 || c > 5 {
				continue
			}
			plans = append(plans, map[model.GoodsID]int{
				goods[0]: a,
				goods[1]: b,
				goods[2]: c,
			})
		}
	}
	return plans
}

func (a *EvolvableAgent) shipStartPlanScore(g *model.Game, playerID int, starts map[model.GoodsID]int) float64 {
	if len(starts) == 0 {
		return 0
	}
	value := navigatorBoardValue(g, playerID, starts)
	for _, gid := range g.Round.SelectedGoods {
		arrivalProb := shipArrivalProbability(g, gid, starts)
		payoutBias := float64(model.ShipPayout(gid)) / 36
		ownShares := float64(g.Players[playerID].Shares[gid])
		value += arrivalProb * payoutBias * (0.25 + ownShares*0.15)
	}
	return value
}

func shipStartPlanLess(a, b map[model.GoodsID]int, goods []model.GoodsID) bool {
	if b == nil {
		return true
	}
	for _, gid := range goods {
		if a[gid] != b[gid] {
			return a[gid] > b[gid]
		}
	}
	return false
}

type navigatorMove struct {
	gid   model.GoodsID
	delta int
}

func (a *EvolvableAgent) navigatorMoves(g *model.Game, playerID int) []interface{} {
	moves, score := a.bestNavigatorMove(g, playerID, g.Round.NavigatorStep)
	if score <= 0.15 {
		return []interface{}{}
	}
	out := make([]interface{}, 0, len(moves))
	for _, mv := range moves {
		out = append(out, map[string]interface{}{"shipId": int(mv.gid), "delta": mv.delta})
	}
	return out
}

func (a *EvolvableAgent) bestNavigatorMoveScore(g *model.Game, playerID int, step string) float64 {
	_, score := a.bestNavigatorMove(g, playerID, step)
	return score
}

func (a *EvolvableAgent) bestNavigatorMove(g *model.Game, playerID int, step string) ([]navigatorMove, float64) {
	candidates := legalNavigatorMoveCandidates(g, step)
	bestScore := 0.0
	best := []navigatorMove{}
	for _, moves := range candidates {
		score := a.navigatorMoveScore(g, playerID, moves)
		if score > bestScore || (math.Abs(score-bestScore) < 0.000001 && navigatorMovesLess(moves, best)) {
			bestScore = score
			best = moves
		}
	}
	return best, bestScore
}

func legalNavigatorMoveCandidates(g *model.Game, step string) [][]navigatorMove {
	sailing := []model.GoodsID{}
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship != nil && ship.Status == model.ShipSailing {
			sailing = append(sailing, gid)
		}
	}
	candidates := [][]navigatorMove{}
	for _, gid := range sailing {
		candidates = append(candidates, []navigatorMove{{gid: gid, delta: -1}}, []navigatorMove{{gid: gid, delta: 1}})
	}
	if step != "big" {
		return candidates
	}
	for _, gid := range sailing {
		candidates = append(candidates, []navigatorMove{{gid: gid, delta: -2}}, []navigatorMove{{gid: gid, delta: 2}})
	}
	for i, gidA := range sailing {
		for _, gidB := range sailing[i+1:] {
			for _, deltaA := range []int{-1, 1} {
				for _, deltaB := range []int{-1, 1} {
					candidates = append(candidates, []navigatorMove{{gid: gidA, delta: deltaA}, {gid: gidB, delta: deltaB}})
				}
			}
		}
	}
	return candidates
}

func navigatorMovesLess(a, b []navigatorMove) bool {
	if len(b) == 0 {
		return len(a) > 0
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	for i := range a {
		if a[i].gid != b[i].gid {
			return a[i].gid < b[i].gid
		}
		if a[i].delta != b[i].delta {
			return a[i].delta < b[i].delta
		}
	}
	return false
}

func (a *EvolvableAgent) navigatorMoveScore(g *model.Game, playerID int, moves []navigatorMove) float64 {
	if len(moves) == 0 {
		return 0
	}
	overrides := map[model.GoodsID]int{}
	for _, mv := range moves {
		ship := g.Ships[mv.gid]
		if ship == nil || ship.Status != model.ShipSailing || mv.delta == 0 {
			continue
		}
		overrides[mv.gid] = clampInt(ship.Position+mv.delta, 0, 14)
	}
	if len(overrides) == 0 {
		return 0
	}
	before := navigatorBoardValue(g, playerID, nil)
	after := navigatorBoardValue(g, playerID, overrides)
	return after - before
}

func parseNavigatorMoves(raw interface{}) []navigatorMove {
	items := interfaceSlice(raw)
	moves := make([]navigatorMove, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		gid := model.GoodsID(intFromPayload(m["shipId"], 0))
		delta := intFromPayload(m["delta"], 0)
		if gid == 0 || delta == 0 {
			continue
		}
		moves = append(moves, navigatorMove{gid: gid, delta: delta})
	}
	return moves
}

func parseShipStarts(raw interface{}) map[model.GoodsID]int {
	items, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	starts := map[model.GoodsID]int{}
	for key, value := range items {
		gid := model.GoodsID(intFromPayload(key, 0))
		if gid == 0 {
			continue
		}
		starts[gid] = intFromPayload(value, 0)
	}
	return starts
}

func navigatorBoardValue(g *model.Game, playerID int, overrides map[model.GoodsID]int) float64 {
	value := 0.0
	leader := estimatedLeader(g, playerID)
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship == nil {
			continue
		}
		position := ship.Position
		if overrides != nil {
			if override, ok := overrides[gid]; ok {
				position = override
			}
		}
		arrivalProb := shipArrivalProbability(g, gid, overrides)
		if ship.Status == model.ShipArrived || position > 13 {
			arrivalProb = 1
		}
		if ship.Status == model.ShipDocked {
			arrivalProb = 0
		}
		at13Prob := shipPirateLootProbability(g, gid, overrides)
		ordinarySeats := len(ship.Occupants)
		if ordinarySeats < 1 {
			ordinarySeats = 1
		}
		payout := float64(model.ShipPayout(gid))
		ordinaryShare := payout / float64(ordinarySeats)
		ownCrew := shipOccupantCount(ship, playerID)
		oppCrew := len(ship.Occupants) + len(ship.Stowaways) - ownCrew
		value += arrivalProb * ordinaryShare * (float64(ownCrew) - 0.45*float64(oppCrew))

		ownShares := g.Players[playerID].Shares[gid]
		leaderShares := 0
		if leader != 0 {
			leaderShares = visibleKnownShares(g, playerID, leader, gid)
		}
		value += arrivalProb * float64(ownShares) * marketAdvanceValue(g, gid)
		value -= arrivalProb * float64(leaderShares) * 1.1
		value -= arrivalProb * opponentSharePressure(g, playerID, gid) * 0.35

		if len(g.Board.Pirates) > 0 {
			pirateShare := payout / float64(len(g.Board.Pirates))
			ownPirates := pieceCount(g.Board.Pirates, playerID)
			oppPirates := len(g.Board.Pirates) - ownPirates
			value += at13Prob * pirateShare * (float64(ownPirates) - 0.35*float64(oppPirates))
			value -= at13Prob * ordinaryShare * float64(ownCrew) * 0.45
		}
	}
	value += boardSlotValue(g, playerID, true, overrides)
	value += boardSlotValue(g, playerID, false, overrides)
	return value
}

func randomShipStarts(g *model.Game, r *rand.Rand) map[string]interface{} {
	starts := map[string]interface{}{}
	remaining := 9
	for i, gid := range g.Round.SelectedGoods {
		left := len(g.Round.SelectedGoods) - i - 1
		minForRest := 0
		maxForRest := 5 * left
		minValue := remaining - maxForRest
		if minValue < 0 {
			minValue = 0
		}
		maxValue := remaining - minForRest
		if maxValue > 5 {
			maxValue = 5
		}
		value := minValue
		if maxValue > minValue {
			value += r.Intn(maxValue - minValue + 1)
		}
		starts[fmt.Sprint(int(gid))] = value
		remaining -= value
	}
	return starts
}

func (a *EvolvableAgent) harborMasterValue(g *model.Game, playerID int) float64 {
	shareValue := 5.0
	for _, gid := range model.GoodsOrder {
		gd := g.Goods[gid]
		if gd.SharesRemaining <= 0 {
			continue
		}
		price := gd.MarketValue()
		if price < 5 {
			price = 5
		}
		assetValue := gd.MarketValue() + 5 + g.Players[playerID].Shares[gid]*2
		net := float64(assetValue - price)
		if net > shareValue {
			shareValue = net
		}
	}
	return shareValue*a.Weights.AuctionShareValue +
		a.harborControlValue(g, playerID)*a.Weights.AuctionControlValue +
		a.harborFirstMoveValue(g, playerID)*a.Weights.AuctionFirstMoveValue
}

func (a *EvolvableAgent) harborControlValue(g *model.Game, playerID int) float64 {
	type item struct {
		gid   model.GoodsID
		score float64
	}
	items := make([]item, 0, len(model.GoodsOrder))
	leader := estimatedLeader(g, playerID)
	for _, gid := range model.GoodsOrder {
		ownShares := float64(g.Players[playerID].Shares[gid])
		leaderShares := 0.0
		if leader != 0 {
			leaderShares = float64(visibleKnownShares(g, playerID, leader, gid))
		}
		opponentShares := opponentSharePressure(g, playerID, gid)
		marketGain := marketAdvanceValue(g, gid)
		score := ownShares*marketGain -
			leaderShares*marketGain*0.75 -
			opponentShares*marketGain*0.2 +
			float64(model.ShipPayout(gid))/36
		items = append(items, item{gid: gid, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].gid < items[j].gid
		}
		return items[i].score > items[j].score
	})
	value := 0.0
	for i := 0; i < 3 && i < len(items); i++ {
		value += math.Max(0, items[i].score)
	}
	if len(items) >= 3 {
		value += math.Max(0, items[0].score-items[2].score) * 0.35
	}
	return value
}

func (a *EvolvableAgent) harborFirstMoveValue(g *model.Game, playerID int) float64 {
	if g.Players[playerID] == nil || g.Players[playerID].AvailablePieces <= 0 {
		return 0
	}
	value := 2.0 + a.leaderAssetPressure(g, playerID)
	for _, gid := range model.GoodsOrder {
		value += float64(g.Players[playerID].Shares[gid]) * marketAdvanceValue(g, gid) * 0.08
	}
	return value
}

func (a *EvolvableAgent) selectedOwnSharePressure(g *model.Game, playerID int) float64 {
	total := 0.0
	for _, gid := range g.Round.SelectedGoods {
		total += float64(g.Players[playerID].Shares[gid])
	}
	return total
}

func (a *EvolvableAgent) leaderAssetPressure(g *model.Game, playerID int) float64 {
	leader := estimatedLeader(g, playerID)
	if leader == 0 {
		return 0
	}
	total := 0.0
	for _, gid := range model.GoodsOrder {
		total += float64(visibleKnownShares(g, playerID, leader, gid) - g.Players[playerID].Shares[gid])
	}
	return math.Max(0, total/4)
}

func (a *EvolvableAgent) pirateLeaderDenial(g *model.Game, playerID int) float64 {
	leader := estimatedLeader(g, playerID)
	if leader == 0 {
		return 0
	}
	value := 0.0
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship == nil || ship.Status != model.ShipSailing {
			continue
		}
		value += (shipPirateLootProbability(g, gid) + shipPirateBoardingProbability(g, gid)*pirateBoardingWeight) * float64(visibleKnownShares(g, playerID, leader, gid))
	}
	return value
}

func shipArrivalProbability(g *model.Game, gid model.GoodsID, overrides map[model.GoodsID]int) float64 {
	ship := g.Ships[gid]
	if ship == nil {
		return 0
	}
	if ship.Status == model.ShipArrived {
		return 1
	}
	if ship.Status == model.ShipDocked {
		return 0
	}
	position := ship.Position
	if overrides != nil {
		if override, ok := overrides[gid]; ok {
			position = override
		}
	}
	rolls := remainingRolls(g)
	naturalArrival := moveTailProbability(rolls, 14-position)
	exact13 := moveSumProbability(rolls, 13-position)
	weight := 1.0
	if len(g.Board.Pirates) > 0 {
		weight = piratePortWeight
	}
	return clamp(naturalArrival+exact13*weight, 0, 1)
}

func shipPirateBoardingProbability(g *model.Game, gid model.GoodsID) float64 {
	ship := g.Ships[gid]
	if ship == nil || ship.Status != model.ShipSailing || g.Round.MovementStep >= 2 {
		return 0
	}
	return exactPositionProbability(ship.Position, 13, 2-g.Round.MovementStep)
}

func shipPirateLootProbability(g *model.Game, gid model.GoodsID, overrides ...map[model.GoodsID]int) float64 {
	ship := g.Ships[gid]
	if ship == nil || ship.Status != model.ShipSailing {
		return 0
	}
	position := ship.Position
	if len(overrides) > 0 && overrides[0] != nil {
		if override, ok := overrides[0][gid]; ok {
			position = override
		}
	}
	return exactPositionProbability(position, 13, remainingRolls(g))
}

func exactPositionProbability(position int, target int, rolls int) float64 {
	return moveSumProbability(rolls, target-position)
}

type moveProbabilityTable struct {
	prob []float64
	tail []float64
}

func buildMoveProbabilityTables(maxRolls int) []moveProbabilityTable {
	tables := make([]moveProbabilityTable, maxRolls+1)
	for rolls := 0; rolls <= maxRolls; rolls++ {
		maxSum := rolls * 6
		prob := make([]float64, maxSum+1)
		if rolls == 0 {
			prob[0] = 1
		} else {
			counts := map[int]int{0: 1}
			for i := 0; i < rolls; i++ {
				next := map[int]int{}
				for sum, count := range counts {
					for face := 1; face <= 6; face++ {
						next[sum+face] += count
					}
				}
				counts = next
			}
			total := math.Pow(6, float64(rolls))
			for sum, count := range counts {
				prob[sum] = float64(count) / total
			}
		}
		tail := make([]float64, maxSum+2)
		for sum := maxSum; sum >= 0; sum-- {
			tail[sum] = tail[sum+1] + prob[sum]
		}
		tables[rolls] = moveProbabilityTable{prob: prob, tail: tail}
	}
	return tables
}

func remainingRolls(g *model.Game) int {
	return clampInt(3-g.Round.MovementStep, 0, len(moveProbabilityTables)-1)
}

func moveSumProbability(rolls int, sum int) float64 {
	rolls = clampInt(rolls, 0, len(moveProbabilityTables)-1)
	table := moveProbabilityTables[rolls]
	if sum < 0 || sum >= len(table.prob) {
		return 0
	}
	return table.prob[sum]
}

func moveTailProbability(rolls int, minSum int) float64 {
	rolls = clampInt(rolls, 0, len(moveProbabilityTables)-1)
	table := moveProbabilityTables[rolls]
	if minSum <= 0 {
		return 1
	}
	if minSum >= len(table.tail) {
		return 0
	}
	return table.tail[minSum]
}

func slotHitProbability(slotID string, port bool, g *model.Game) float64 {
	return slotHitProbabilityWithOverrides(slotID, port, g, nil)
}

func slotHitProbabilityWithOverrides(slotID string, port bool, g *model.Game, overrides map[model.GoodsID]int) float64 {
	targetIndex := map[string]int{"A": 0, "B": 1, "C": 2}[slotID]
	if port {
		if targetIndex < len(g.Round.ArrivedOrder) {
			return 1
		}
		return nthSlotProbability(shipOutcomeProbabilitiesWithOverrides(g, true, overrides), targetIndex-len(g.Round.ArrivedOrder))
	}
	if targetIndex < len(g.Round.DockedOrder) {
		return 1
	}
	return nthSlotProbability(shipOutcomeProbabilitiesWithOverrides(g, false, overrides), targetIndex-len(g.Round.DockedOrder))
}

func shipOutcomeProbabilities(g *model.Game, wantArrival bool) []float64 {
	return shipOutcomeProbabilitiesWithOverrides(g, wantArrival, nil)
}

func shipOutcomeProbabilitiesWithOverrides(g *model.Game, wantArrival bool, overrides map[model.GoodsID]int) []float64 {
	probs := []float64{}
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship == nil {
			continue
		}
		switch ship.Status {
		case model.ShipArrived:
			continue
		case model.ShipDocked:
			continue
		default:
			arrivalProb := shipArrivalProbability(g, gid, overrides)
			if wantArrival {
				probs = append(probs, arrivalProb)
			} else {
				probs = append(probs, 1-arrivalProb)
			}
		}
	}
	return probs
}

func boardSlotValue(g *model.Game, playerID int, port bool, overrides map[model.GoodsID]int) float64 {
	value := 0.0
	slots := g.Board.Ports
	if !port {
		slots = g.Board.Docks
	}
	leader := estimatedLeader(g, playerID)
	for _, slotID := range []string{"A", "B", "C"} {
		slot := slots[slotID]
		if slot == nil || slot.Occupant == nil {
			continue
		}
		prob := slotHitProbabilityWithOverrides(slotID, port, g, overrides)
		reward := float64(slot.Reward)
		if slot.Occupant.PlayerID == playerID {
			value += prob * reward
			continue
		}
		denial := 0.35
		if leader != 0 && slot.Occupant.PlayerID == leader {
			denial = 0.6
		}
		value -= prob * reward * denial
	}
	return value
}

func marketAdvanceValue(g *model.Game, gid model.GoodsID) float64 {
	goods := g.Goods[gid]
	if goods.MarketValueIndex >= len(model.MarketValues)-1 {
		return 0
	}
	return float64(model.MarketValues[goods.MarketValueIndex+1] - model.MarketValues[goods.MarketValueIndex])
}

func nthSlotProbability(probs []float64, targetIndex int) float64 {
	if targetIndex < 0 {
		return 0
	}
	dist := []float64{1, 0, 0, 0}
	for _, p := range probs {
		p = clamp(p, 0, 1)
		next := make([]float64, len(dist))
		for count, current := range dist {
			next[count] += current * (1 - p)
			if count+1 < len(next) {
				next[count+1] += current * p
			}
		}
		dist = next
	}
	if targetIndex >= len(dist) {
		return 0
	}
	total := 0.0
	for successes := targetIndex + 1; successes < len(dist); successes++ {
		total += dist[successes]
	}
	return clamp(total, 0, 1)
}

func cashSafety(p *model.Player, cost int) float64 {
	after := maxPayable(p) - cost
	return clamp(float64(after)/24, -1, 1)
}

func flexibilityAfterCost(p *model.Player, cost int) float64 {
	after := maxPayable(p) - cost
	return clamp(float64(after)/36, 0, 1)
}

func maxPayable(p *model.Player) int {
	if p == nil {
		return 0
	}
	return p.Cash + unmortgagedShares(p)*12
}

func unmortgagedShares(p *model.Player) int {
	total := 0
	for _, gid := range model.GoodsOrder {
		total += p.Shares[gid]
	}
	unmortgaged := total - p.MortgagedShareCount
	if unmortgaged < 0 {
		return 0
	}
	return unmortgaged
}

func opponentSharePressure(g *model.Game, playerID int, gid model.GoodsID) float64 {
	total := 0
	for _, pid := range model.PlayerOrder {
		if pid != playerID {
			total += visibleKnownShares(g, playerID, pid, gid)
		}
	}
	return float64(total)
}

func leaderSharePressure(g *model.Game, playerID int, gid model.GoodsID) float64 {
	leader := estimatedLeader(g, playerID)
	if leader == 0 {
		return 0
	}
	return float64(visibleKnownShares(g, playerID, leader, gid))
}

func estimatedLeader(g *model.Game, viewer int) int {
	bestPlayer := 0
	bestWealth := math.Inf(-1)
	for _, pid := range model.PlayerOrder {
		if pid == viewer {
			continue
		}
		wealth := visibleEstimatedWealth(g, viewer, pid)
		if bestPlayer == 0 || wealth > bestWealth {
			bestPlayer = pid
			bestWealth = wealth
		}
	}
	return bestPlayer
}

func estimatedRank(g *model.Game, playerID int) int {
	own := visibleEstimatedWealth(g, playerID, playerID)
	rank := 1
	for _, pid := range model.PlayerOrder {
		if pid != playerID && visibleEstimatedWealth(g, playerID, pid) > own {
			rank++
		}
	}
	return rank
}

func visibleLeadTrailRatios(g *model.Game, playerID int) (float64, float64) {
	own := visibleEstimatedWealth(g, playerID, playerID)
	total := 0.0
	count := 0.0
	for _, pid := range model.PlayerOrder {
		total += visibleEstimatedWealth(g, playerID, pid)
		count++
	}
	if count == 0 {
		return 0, 0
	}
	average := total / count
	gap := own - average
	if gap >= 0 {
		return clamp(gap/80, 0, 1), 0
	}
	return 0, clamp(-gap/80, 0, 1)
}

func exactEstimatedWealth(g *model.Game, playerID int) float64 {
	p := g.Players[playerID]
	if p == nil {
		return 0
	}
	wealth := float64(p.Cash)
	for _, gid := range model.GoodsOrder {
		wealth += float64(p.Shares[gid] * g.Goods[gid].MarketValue())
	}
	wealth -= float64(p.MortgagedShareCount * 15)
	return wealth
}

func visibleEstimatedWealth(g *model.Game, viewer int, target int) float64 {
	if target == viewer {
		return exactEstimatedWealth(g, target)
	}
	p := g.Players[target]
	if p == nil {
		return 0
	}
	wealth := float64(p.Cash)
	for _, gid := range model.GoodsOrder {
		wealth += float64(visibleBoughtShares(g, target, gid) * g.Goods[gid].MarketValue())
	}
	wealth += float64(visibleHiddenShareCount(g, viewer, target)) * hiddenAverageValue(g, viewer)
	wealth -= float64(p.MortgagedShareCount * 15)
	return wealth
}

func visibleKnownShares(g *model.Game, viewer int, target int, gid model.GoodsID) int {
	if target == viewer {
		if p := g.Players[target]; p != nil {
			return p.Shares[gid]
		}
		return 0
	}
	return visibleBoughtShares(g, target, gid)
}

func visibleBoughtShares(g *model.Game, playerID int, gid model.GoodsID) int {
	count := 0
	for _, event := range g.Events {
		if event.Type != "ShareBought" || event.Data == nil {
			continue
		}
		if intFromPayload(event.Data["playerId"], 0) != playerID {
			continue
		}
		if model.GoodsID(intFromPayload(event.Data["goodsId"], 0)) != gid {
			continue
		}
		count++
	}
	return count
}

func totalVisibleBoughtShares(g *model.Game, gid model.GoodsID) int {
	total := 0
	for _, pid := range model.PlayerOrder {
		total += visibleBoughtShares(g, pid, gid)
	}
	return total
}

func viewerInitialHiddenShares(g *model.Game, viewer int, gid model.GoodsID) int {
	p := g.Players[viewer]
	if p == nil {
		return 0
	}
	hidden := p.Shares[gid] - visibleBoughtShares(g, viewer, gid)
	if hidden < 0 {
		return 0
	}
	return hidden
}

func hiddenPoolByGoods(g *model.Game, viewer int) map[model.GoodsID]int {
	pool := map[model.GoodsID]int{}
	for _, gid := range model.GoodsOrder {
		goods := g.Goods[gid]
		remaining := 0
		if goods != nil {
			remaining = goods.SharesRemaining
		}
		hidden := totalSharesPerGoods - remaining - totalVisibleBoughtShares(g, gid)
		hidden -= viewerInitialHiddenShares(g, viewer, gid)
		if hidden < 0 {
			hidden = 0
		}
		pool[gid] = hidden
	}
	return pool
}

func hiddenPoolCount(g *model.Game, viewer int) int {
	total := 0
	for _, count := range hiddenPoolByGoods(g, viewer) {
		total += count
	}
	return total
}

func hiddenAverageValue(g *model.Game, viewer int) float64 {
	pool := hiddenPoolByGoods(g, viewer)
	totalCount := 0
	totalValue := 0
	for _, gid := range model.GoodsOrder {
		count := pool[gid]
		totalCount += count
		totalValue += count * g.Goods[gid].MarketValue()
	}
	if totalCount > 0 {
		return float64(totalValue) / float64(totalCount)
	}
	fallback := 0
	for _, gid := range model.GoodsOrder {
		fallback += g.Goods[gid].MarketValue()
	}
	return float64(fallback) / float64(len(model.GoodsOrder))
}

func visibleHiddenShareCount(g *model.Game, viewer int, target int) int {
	if target == viewer {
		return 0
	}
	return clampInt(initialHiddenSharesPerPlayer, 0, hiddenPoolCount(g, viewer))
}

func maxMarketIndex(g *model.Game) int {
	max := 0
	for _, gid := range model.GoodsOrder {
		if g.Goods[gid].MarketValueIndex > max {
			max = g.Goods[gid].MarketValueIndex
		}
	}
	return max
}

func shipOccupantCount(ship *model.Ship, playerID int) int {
	total := 0
	for _, occ := range ship.Occupants {
		if occ.PlayerID == playerID {
			total++
		}
	}
	for _, st := range ship.Stowaways {
		if st.PlayerID == playerID {
			total++
		}
	}
	return total
}

func pieceCount(pieces []model.Piece, playerID int) int {
	total := 0
	for _, piece := range pieces {
		if piece.PlayerID == playerID {
			total++
		}
	}
	return total
}

func copyPayload(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func interfaceSlice(v interface{}) []interface{} {
	if v == nil {
		return nil
	}
	if s, ok := v.([]interface{}); ok {
		return s
	}
	return nil
}

func intFromPayload(v interface{}, fallback int) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case model.GoodsID:
		return int(t)
	case string:
		n, err := strconv.Atoi(t)
		if err == nil {
			return n
		}
		return fallback
	default:
		return fallback
	}
}

func linear(gene, min, max float64) float64 {
	return min + clamp01(gene)*(max-min)
}

func logLinear(gene, min, max float64) float64 {
	return math.Exp(math.Log(min) + clamp01(gene)*(math.Log(max)-math.Log(min)))
}

func clamp01(v float64) float64 {
	return clamp(v, 0, 1)
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
