package ai

import (
	"math"
	"math/rand"
	"testing"

	"manila/internal/model"
	"manila/internal/rules"
)

func TestDecodeGenomeRanges(t *testing.T) {
	weights := DecodeGenome(Genome{Genes: []float64{}})
	if weights.Temperature < 0.05 || weights.Temperature > 2.0 {
		t.Fatalf("temperature outside decoded range: %f", weights.Temperature)
	}
	if weights.NavigatorValue != 1.0 {
		t.Fatalf("missing navigator gene should default to neutral 1.0, got %f", weights.NavigatorValue)
	}
	low := DecodeGenome(Genome{Genes: []float64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}})
	high := DecodeGenome(Genome{Genes: []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1}})
	if low.Temperature >= high.Temperature {
		t.Fatalf("expected log temperature mapping to increase: low=%f high=%f", low.Temperature, high.Temperature)
	}
	if DecodeGenome(Genome{Genes: []float64{-10}}).WCash < -1.5 {
		t.Fatalf("expected genes to be clamped")
	}
}

func TestGeneNamesMatchGeneCount(t *testing.T) {
	if len(GeneNames) != GeneCount {
		t.Fatalf("expected %d gene names, got %d", GeneCount, len(GeneNames))
	}
	if index, ok := GeneIndex("auction_price_sensitivity"); !ok || index != 21 {
		t.Fatalf("expected auction_price_sensitivity at index 21, got %d ok=%v", index, ok)
	}
	if index, ok := GeneIndex("cargo_visible_leader_deny_weight"); !ok || index != 26 {
		t.Fatalf("expected cargo_visible_leader_deny_weight at index 26, got %d ok=%v", index, ok)
	}
}

func TestMutationProfileFocusesGenes(t *testing.T) {
	parent := NeutralGenome()
	r := rand.New(rand.NewSource(1))
	focusIndex := GeneCount - 1
	child := MutateGenomeWithProfile(parent, r, 0.000000001, MutationProfile{
		FocusGenes:            map[int]bool{focusIndex: true},
		BaseResetProbability:  0.000001,
		FocusResetProbability: 1,
		FocusSigmaMultiplier:  1,
	})
	for i := 0; i < GeneCount; i++ {
		if i == focusIndex {
			continue
		}
		if math.Abs(child.Genes[i]-parent.Genes[i]) > 0.000001 {
			t.Fatalf("non-focused gene %d changed: %f -> %f", i, parent.Genes[i], child.Genes[i])
		}
	}
	if child.Genes[focusIndex] == parent.Genes[focusIndex] {
		t.Fatalf("focused gene did not change")
	}
}

func TestSoftmaxLowTemperaturePrefersHighScore(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	counts := map[int]int{}
	for i := 0; i < 100; i++ {
		counts[SoftmaxChoice([]float64{0, 10}, 0.05, r)]++
	}
	if counts[1] != 100 {
		t.Fatalf("expected high score to dominate at low temperature, got counts %+v", counts)
	}
}

func TestEvolvableAgentReturnsApplicableActions(t *testing.T) {
	engine := rules.NewEngine()
	g := rules.NewGame("ai-validity", 7)
	if err := engine.StartGame(g); err != nil {
		t.Fatal(err)
	}
	agent := NewEvolvableAgent(NeutralGenome(), 99)
	for step := 0; step < 80 && g.Status != model.StatusEnded; step++ {
		action, err := agent.ChooseAction(engine, g, g.CurrentPlayer)
		if err != nil {
			t.Fatalf("choose action failed at step %d phase %s: %v", step, g.Phase, err)
		}
		if action.Type == model.ActionBid && action.Payload["amount"] == nil {
			t.Fatalf("bid action missing amount")
		}
		if action.Type == model.ActionSelectGoods && action.Payload["goodsIds"] == nil {
			t.Fatalf("select goods action missing goodsIds")
		}
		if action.Type == model.ActionSetShipStarts && action.Payload["starts"] == nil {
			t.Fatalf("set starts action missing starts")
		}
		if action.Type == model.ActionNavigatorMove && action.Payload["moves"] == nil {
			t.Fatalf("navigator move action missing moves")
		}
		if err := engine.ApplyAction(g, action); err != nil {
			t.Fatalf("apply action failed at step %d phase %s action %+v: %v", step, g.Phase, action, err)
		}
	}
}

func TestOldWeightsGetNeutralAuctionDefaults(t *testing.T) {
	agent := NewWeightedAgent(Weights{}, 1)
	if agent.Weights.AuctionShareValue != 1.0 ||
		agent.Weights.AuctionControlValue != 1.0 ||
		agent.Weights.AuctionFirstMoveValue != 1.0 ||
		agent.Weights.AuctionPriceSensitivity != 1.0 ||
		agent.Weights.CargoOwnShareWeight == 0 ||
		agent.Weights.CargoLeadingHighValue == 0 ||
		agent.Weights.CargoTrailingLowValue == 0 ||
		agent.Weights.CargoTrailingEndPenalty == 0 ||
		agent.Weights.CargoLeaderDeny == 0 {
		t.Fatalf("old weights should receive neutral auction defaults: %+v", agent.Weights)
	}
}

func TestPreviousHarborMasterOpeningBidFiltersPass(t *testing.T) {
	engine := rules.NewEngine()
	g := rules.NewGame("opening-bid", 11)
	if err := engine.StartGame(g); err != nil {
		t.Fatal(err)
	}
	if g.CurrentPlayer != g.PreviousHarborMaster || g.Auction.HasAnyBid {
		t.Fatalf("test expected previous harbor master to open auction")
	}
	agent := NewEvolvableAgent(NeutralGenome(), 1)
	action, err := agent.ChooseAction(engine, g, g.CurrentPlayer)
	if err != nil {
		t.Fatal(err)
	}
	if action.Type == model.ActionPassBid {
		t.Fatalf("previous harbor master should not pass before any bid")
	}
	if action.Type != model.ActionBid {
		t.Fatalf("expected opening bid, got %s", action.Type)
	}
}

func TestBidActionUsesMinimumRaiseWhenReservationAllows(t *testing.T) {
	g := rules.NewGame("bid-candidates", 12)
	resetTestShares(g)
	agent := NewEvolvableAgent(NeutralGenome(), 1)
	legal := model.LegalAction{
		Type:    model.ActionBid,
		Payload: map[string]interface{}{"minBid": 1, "maxBid": 40},
	}
	actions := agent.completeActions(g, 1, legal)
	if len(actions) != 1 {
		t.Fatalf("expected one minimum raise candidate, got %d", len(actions))
	}
	if amount := intFromPayload(actions[0].Payload["amount"], 0); amount != 1 {
		t.Fatalf("expected minimum bid 1, got %d", amount)
	}
}

func TestAuctionBidsUntilReservationInsteadOfScoringPass(t *testing.T) {
	engine := rules.NewEngine()
	g := rules.NewGame("auction-min-raise", 15)
	resetTestShares(g)
	g.Status = model.StatusInProgress
	g.Phase = model.PhaseAuction
	g.Auction.CurrentBid = 2
	g.Auction.HighestBidder = 1
	g.Auction.HasAnyBid = true
	g.CurrentPlayer = 2
	g.Players[2].Cash = 55

	agent := NewWeightedAgent(Weights{
		WCash:                   2.0,
		WFlexibility:            1.5,
		Temperature:             0.05,
		BidValueMultiplier:      1.0,
		BidCashCap:              1.0,
		ReserveCashRatio:        0.0,
		AuctionShareValue:       1.0,
		AuctionControlValue:     1.0,
		AuctionFirstMoveValue:   1.0,
		AuctionPriceSensitivity: 1.0,
	}, 1)
	action, err := agent.ChooseAction(engine, g, 2)
	if err != nil {
		t.Fatal(err)
	}
	if action.Type != model.ActionBid {
		t.Fatalf("expected bid while minimum raise is within reservation, got %s", action.Type)
	}
	if amount := intFromPayload(action.Payload["amount"], 0); amount != 3 {
		t.Fatalf("expected minimum raise to 3, got %d", amount)
	}
}

func TestVisibleSharePressureIgnoresOpponentHiddenShares(t *testing.T) {
	g := rules.NewGame("visible-shares", 13)
	resetTestShares(g)
	g.Players[2].Shares[model.GoodsGinseng] = 2

	if pressure := opponentSharePressure(g, 1, model.GoodsGinseng); pressure != 0 {
		t.Fatalf("hidden opponent shares should not be visible pressure, got %f", pressure)
	}

	addPublicShareBuy(g, 2, model.GoodsGinseng)
	if pressure := opponentSharePressure(g, 1, model.GoodsGinseng); pressure != 1 {
		t.Fatalf("publicly bought opponent share should be visible pressure, got %f", pressure)
	}
}

func TestVisibleWealthUsesHiddenPoolAverageInsteadOfPeeking(t *testing.T) {
	g := rules.NewGame("visible-wealth", 14)
	resetTestShares(g)
	g.Goods[model.GoodsGinseng].MarketValueIndex = 4
	g.Goods[model.GoodsJade].MarketValueIndex = 1
	g.Goods[model.GoodsGinseng].SharesRemaining = 3
	g.Goods[model.GoodsJade].SharesRemaining = 3
	g.Players[2].Shares[model.GoodsGinseng] = 2
	g.Players[3].Shares[model.GoodsJade] = 2

	p2 := visibleEstimatedWealth(g, 1, 2)
	p3 := visibleEstimatedWealth(g, 1, 3)
	if !closeFloat(p2, p3) {
		t.Fatalf("hidden share types should not be peeked at: p2=%f p3=%f", p2, p3)
	}

	own := visibleEstimatedWealth(g, 2, 2)
	if own <= p2 {
		t.Fatalf("a player should still know their own exact hidden shares: own=%f visible=%f", own, p2)
	}
}

func TestSelectGoodsPrefersHighValueCargoWhenLeading(t *testing.T) {
	g := rules.NewGame("leading-cargo", 16)
	resetTestShares(g)
	g.Players[1].Cash = 300
	g.Goods[model.GoodsSilk].MarketValueIndex = 3
	g.Goods[model.GoodsJade].MarketValueIndex = 3

	agent := NewWeightedAgent(Weights{
		CargoOwnShareWeight:     0.5,
		CargoLeadingHighValue:   2.0,
		CargoTrailingLowValue:   0.1,
		CargoTrailingEndPenalty: 0.1,
		CargoLeaderDeny:         0.0,
	}, 1)
	selected := goodsIDsFromInterfaces(agent.selectGoods(g, 1))
	if !containsGoods(selected, model.GoodsSilk) || !containsGoods(selected, model.GoodsJade) {
		t.Fatalf("leading player should include high-value cargo, got %+v", selected)
	}
	if containsGoods(selected, model.GoodsGinseng) {
		t.Fatalf("leading high-value preference should exclude lowest cargo, got %+v", selected)
	}
}

func TestSelectGoodsCanExcludeOwnedEndgameCargoWhenTrailing(t *testing.T) {
	g := rules.NewGame("trailing-cargo", 17)
	resetTestShares(g)
	g.Players[1].Cash = 0
	g.Players[2].Cash = 200
	g.Players[3].Cash = 200
	g.Players[4].Cash = 200
	g.Players[1].Shares[model.GoodsJade] = 3
	g.Goods[model.GoodsJade].MarketValueIndex = 3

	agent := NewWeightedAgent(Weights{
		CargoOwnShareWeight:     0.5,
		CargoLeadingHighValue:   0.0,
		CargoTrailingLowValue:   2.0,
		CargoTrailingEndPenalty: 3.0,
		CargoLeaderDeny:         0.0,
	}, 1)
	selected := goodsIDsFromInterfaces(agent.selectGoods(g, 1))
	if containsGoods(selected, model.GoodsJade) {
		t.Fatalf("trailing player should be able to exclude owned endgame cargo, got %+v", selected)
	}
}

func TestNavigatorValueMultiplierAffectsBuyingNavigator(t *testing.T) {
	g := rules.NewGame("navigator-weight", 9)
	setupShipsForProbabilityTest(g, []int{10, 3, 3}, 1)
	resetTestShares(g)
	g.Players[1].Shares[model.GoodsGinseng] = 2
	g.Ships[model.GoodsGinseng].Occupants = []model.Piece{{PlayerID: 1}}

	legal := model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorSmall", "targetId": "small"}, Cost: 2}
	action := model.Action{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: legal.Payload}
	low := NewWeightedAgent(Weights{NavigatorValue: 0.2}, 1).Features(g, 1, legal, action)
	high := NewWeightedAgent(Weights{NavigatorValue: 1.8}, 1).Features(g, 1, legal, action)
	if high.ExpectedValue <= low.ExpectedValue || high.Flexibility <= low.Flexibility {
		t.Fatalf("navigator multiplier should raise navigator buying features: low=%+v high=%+v", low, high)
	}
}

func TestSlotHitProbabilityUsesShipPositions(t *testing.T) {
	g := rules.NewGame("slot-prob", 3)
	setupShipsForProbabilityTest(g, []int{3, 3, 3}, 0)
	earlyDockC := slotHitProbability("C", false, g)
	if earlyDockC >= 0.35 {
		t.Fatalf("early dock C should not be treated as a fixed high-probability bet, got %f", earlyDockC)
	}

	setupShipsForProbabilityTest(g, []int{1, 1, 1}, 2)
	lateDockC := slotHitProbability("C", false, g)
	if lateDockC <= earlyDockC {
		t.Fatalf("late dock C should rise when all ships are unlikely to arrive: early=%f late=%f", earlyDockC, lateDockC)
	}

	setupShipsForProbabilityTest(g, []int{13, 13, 13}, 0)
	portA := slotHitProbability("A", true, g)
	if portA < 0.8 {
		t.Fatalf("port A should be attractive when ships are close to arrival, got %f", portA)
	}
}

func TestArrivalProbabilityUsesDiceTailAndPirate13Weight(t *testing.T) {
	g := rules.NewGame("arrival-prob", 4)
	setupShipsForProbabilityTest(g, []int{8, 3, 3}, 2)
	noPirate := shipArrivalProbability(g, model.GoodsGinseng, nil)
	if !closeFloat(noPirate, 1.0/3.0) {
		t.Fatalf("without pirates, position 8 with one roll should arrive on 5 or 6, got %f", noPirate)
	}

	g.Board.Pirates = []model.Piece{{PlayerID: 4, Role: "captain"}}
	withPirate := shipArrivalProbability(g, model.GoodsGinseng, nil)
	if !closeFloat(withPirate, 0.25) {
		t.Fatalf("with pirates, position 8 should count 6 plus half of 5, got %f", withPirate)
	}
}

func TestSlotProbabilitiesUseIndependentPortAndDockOrders(t *testing.T) {
	g := rules.NewGame("slot-order", 5)
	setupShipsForProbabilityTest(g, []int{1, 14, 1}, 2)
	g.Ships[model.GoodsGinseng].Status = model.ShipDocked
	g.Round.DockedOrder = []model.GoodsID{model.GoodsGinseng}
	portA := slotHitProbability("A", true, g)
	if portA < 0.99 {
		t.Fatalf("a docked ship should not make port A impossible when another ship will arrive, got %f", portA)
	}

	setupShipsForProbabilityTest(g, []int{14, 1, 1}, 2)
	g.Ships[model.GoodsGinseng].Status = model.ShipArrived
	g.Round.ArrivedOrder = []model.GoodsID{model.GoodsGinseng}
	dockA := slotHitProbability("A", false, g)
	if dockA < 0.99 {
		t.Fatalf("an arrived ship should not make dock A impossible when another ship will fail, got %f", dockA)
	}
}

func TestPortCProbabilityCountsPirate13AsHalfPort(t *testing.T) {
	g := rules.NewGame("port-c-pirate", 6)
	setupShipsForProbabilityTest(g, []int{8, 12, 15}, 2)
	g.Ships[model.GoodsSilk].Status = model.ShipArrived
	g.Round.ArrivedOrder = []model.GoodsID{model.GoodsSilk}
	g.Board.Pirates = []model.Piece{{PlayerID: 4, Role: "captain"}}
	portC := slotHitProbability("C", true, g)
	want := 0.25 * (11.0 / 12.0)
	if !closeFloat(portC, want) {
		t.Fatalf("port C should require both remaining ships, got %f want %f", portC, want)
	}
}

func TestPirateProbabilitiesUseMoveTables(t *testing.T) {
	g := rules.NewGame("pirate-probs", 7)
	setupShipsForProbabilityTest(g, []int{8, 12, 3}, 1)
	boarding := shipPirateBoardingProbability(g, model.GoodsGinseng)
	if !closeFloat(boarding, 1.0/6.0) {
		t.Fatalf("position 8 before second roll should board on 5, got %f", boarding)
	}
	loot := shipPirateLootProbability(g, model.GoodsNutmeg)
	if loot != 0 {
		t.Fatalf("position 12 with two rolls cannot end at 13, got %f", loot)
	}

	setupShipsForProbabilityTest(g, []int{12, 3, 3}, 2)
	loot = shipPirateLootProbability(g, model.GoodsGinseng)
	if !closeFloat(loot, 1.0/6.0) {
		t.Fatalf("position 12 before final roll should loot on 1, got %f", loot)
	}
	boarding = shipPirateBoardingProbability(g, model.GoodsGinseng)
	if boarding != 0 {
		t.Fatalf("boarding opportunity is over after second movement, got %f", boarding)
	}
}

func TestPiratePlacementFeaturesUseBoardState(t *testing.T) {
	g := rules.NewGame("pirate-features", 5)
	setupShipsForProbabilityTest(g, []int{3, 3, 3}, 2)
	agent := NewEvolvableAgent(NeutralGenome(), 1)
	legal := model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}, Cost: 5}
	action := model.Action{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: legal.Payload}
	noThreat := agent.Features(g, 1, legal, action)

	setupShipsForProbabilityTest(g, []int{12, 3, 3}, 2)
	g.Board.Pirates = nil
	captain := agent.Features(g, 1, legal, action)
	if captain.ExpectedValue <= noThreat.ExpectedValue {
		t.Fatalf("pirate value should rise when a ship can end at 13: noThreat=%+v captain=%+v", noThreat, captain)
	}

	g.Board.Pirates = []model.Piece{{PlayerID: 2, Role: "captain"}}
	mateLegal := model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}, Cost: 5}
	mateAction := model.Action{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: mateLegal.Payload}
	mate := agent.Features(g, 1, mateLegal, mateAction)
	if mate.ExpectedValue >= captain.ExpectedValue {
		t.Fatalf("second pirate should be diluted compared with captain slot: captain=%+v mate=%+v", captain, mate)
	}
}

func closeFloat(a, b float64) bool {
	return math.Abs(a-b) < 0.000001
}

func TestInsuranceFeaturesUseDockRisk(t *testing.T) {
	g := rules.NewGame("insurance-features", 6)
	setupShipsForProbabilityTest(g, []int{3, 3, 3}, 0)
	agent := NewEvolvableAgent(NeutralGenome(), 1)
	legal := model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}, Cost: 0}
	action := model.Action{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: legal.Payload}
	noDockBet := agent.Features(g, 1, legal, action)

	setupShipsForProbabilityTest(g, []int{1, 1, 1}, 2)
	g.Board.Docks["C"].Occupant = &model.Piece{PlayerID: 2}
	risky := agent.Features(g, 1, legal, action)
	if risky.ExpectedValue >= noDockBet.ExpectedValue {
		t.Fatalf("insurance should lose value when dock payout is likely: noDock=%+v risky=%+v", noDockBet, risky)
	}
	if risky.TailLoss >= noDockBet.TailLoss {
		t.Fatalf("insurance tail loss should worsen with dock liability: noDock=%+v risky=%+v", noDockBet, risky)
	}
}

func TestCertainProfitablePortIsValuedAboveSkipping(t *testing.T) {
	g := rules.NewGame("profitable-port", 7)
	setupShipsForProbabilityTest(g, []int{14, 3, 3}, 2)
	g.Round.ArrivedOrder = []model.GoodsID{model.GoodsGinseng}
	agent := NewWeightedAgent(Weights{
		WCash:        1.5,
		WFlexibility: 1.15,
		WVariance:    0.9,
		WUpside:      0.1,
		WUncertainty: -0.75,
		Temperature:  0.05,
	}, 1)

	portLegal := model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}, Cost: 4}
	portAction := model.Action{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: portLegal.Payload}
	portScore := agent.ScoreFeatures(agent.Features(g, 1, portLegal, portAction))

	skipLegal := model.LegalAction{Type: model.ActionPirateSkipBoard}
	skipAction := model.Action{PlayerID: 1, Type: model.ActionPirateSkipBoard}
	skipScore := agent.ScoreFeatures(agent.Features(g, 1, skipLegal, skipAction))
	if portScore <= skipScore {
		t.Fatalf("certain profitable port should beat conservative skip baseline: port=%f skip=%f", portScore, skipScore)
	}
}

func TestShipStartsUseArrivalProbabilityForOwnedShares(t *testing.T) {
	g := rules.NewGame("ship-starts", 7)
	setupShipsForProbabilityTest(g, []int{0, 0, 0}, 0)
	resetTestShares(g)
	g.Players[1].Shares[model.GoodsGinseng] = 3

	agent := NewEvolvableAgent(NeutralGenome(), 1)
	starts := agent.shipStarts(g, 1)
	if intFromPayload(starts["1"], 0) != 5 {
		t.Fatalf("owned ginseng should receive the best start, got %+v", starts)
	}
}

func TestSetShipStartsFeatureScoresBetterProbabilityPlan(t *testing.T) {
	g := rules.NewGame("ship-start-features", 8)
	setupShipsForProbabilityTest(g, []int{0, 0, 0}, 0)
	resetTestShares(g)
	g.Players[1].Shares[model.GoodsGinseng] = 3
	agent := NewEvolvableAgent(NeutralGenome(), 1)
	legal := model.LegalAction{Type: model.ActionSetShipStarts}

	goodAction := model.Action{PlayerID: 1, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 5, "2": 4, "3": 0}}}
	badAction := model.Action{PlayerID: 1, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 0, "2": 4, "3": 5}}}
	good := agent.Features(g, 1, legal, goodAction)
	bad := agent.Features(g, 1, legal, badAction)
	if good.ExpectedValue <= bad.ExpectedValue {
		t.Fatalf("start plan closer to port for owned shares should score higher: good=%+v bad=%+v", good, bad)
	}
}

func TestNavigatorPushesOwnValuableShipForward(t *testing.T) {
	g := rules.NewGame("navigator-push", 7)
	setupShipsForProbabilityTest(g, []int{10, 3, 3}, 1)
	resetTestShares(g)
	g.Players[1].Shares[model.GoodsGinseng] = 2
	g.Ships[model.GoodsGinseng].Occupants = []model.Piece{{PlayerID: 1}}
	g.Round.NavigatorStep = "small"

	agent := NewEvolvableAgent(NeutralGenome(), 1)
	moves := agent.navigatorMoves(g, 1)
	if len(moves) != 1 {
		t.Fatalf("expected one navigator move, got %+v", moves)
	}
	move := moves[0].(map[string]interface{})
	if intFromPayload(move["shipId"], 0) != int(model.GoodsGinseng) || intFromPayload(move["delta"], 0) != 1 {
		t.Fatalf("expected ginseng +1, got %+v", moves)
	}
}

func TestNavigatorPullsOpponentLeaderShipBack(t *testing.T) {
	g := rules.NewGame("navigator-pull", 8)
	setupShipsForProbabilityTest(g, []int{11, 3, 3}, 1)
	resetTestShares(g)
	g.Players[2].Cash = 80
	addPublicShareBuy(g, 2, model.GoodsGinseng)
	addPublicShareBuy(g, 2, model.GoodsGinseng)
	addPublicShareBuy(g, 2, model.GoodsGinseng)
	g.Ships[model.GoodsGinseng].Occupants = []model.Piece{{PlayerID: 2}}
	g.Round.NavigatorStep = "small"

	agent := NewEvolvableAgent(NeutralGenome(), 1)
	moves := agent.navigatorMoves(g, 1)
	if len(moves) != 1 {
		t.Fatalf("expected one navigator move, got %+v", moves)
	}
	move := moves[0].(map[string]interface{})
	if intFromPayload(move["shipId"], 0) != int(model.GoodsGinseng) || intFromPayload(move["delta"], 0) != -1 {
		t.Fatalf("expected ginseng -1, got %+v", moves)
	}
}

func TestBigNavigatorMoveStaysWithinLimit(t *testing.T) {
	g := rules.NewGame("navigator-big", 9)
	setupShipsForProbabilityTest(g, []int{10, 11, 3}, 1)
	resetTestShares(g)
	g.Players[1].Shares[model.GoodsGinseng] = 2
	g.Players[2].Cash = 80
	g.Players[2].Shares[model.GoodsNutmeg] = 3
	g.Ships[model.GoodsGinseng].Occupants = []model.Piece{{PlayerID: 1}}
	g.Ships[model.GoodsNutmeg].Occupants = []model.Piece{{PlayerID: 2}}
	g.Round.NavigatorStep = "big"

	agent := NewEvolvableAgent(NeutralGenome(), 1)
	moves := agent.navigatorMoves(g, 1)
	if len(moves) > 2 {
		t.Fatalf("big navigator moved too many ships: %+v", moves)
	}
	total := 0
	seen := map[int]bool{}
	for _, raw := range moves {
		move := raw.(map[string]interface{})
		shipID := intFromPayload(move["shipId"], 0)
		if seen[shipID] {
			t.Fatalf("ship moved twice: %+v", moves)
		}
		seen[shipID] = true
		total += abs(intFromPayload(move["delta"], 0))
	}
	if total > 2 {
		t.Fatalf("big navigator exceeded total movement: %+v", moves)
	}
}

func setupShipsForProbabilityTest(g *model.Game, positions []int, movementStep int) {
	g.Round.SelectedGoods = []model.GoodsID{model.GoodsGinseng, model.GoodsNutmeg, model.GoodsSilk}
	g.Round.ArrivedOrder = nil
	g.Round.DockedOrder = nil
	g.Round.MovementStep = movementStep
	g.Ships = map[model.GoodsID]*model.Ship{}
	for i, gid := range g.Round.SelectedGoods {
		g.Ships[gid] = &model.Ship{ID: gid, GoodsID: gid, Position: positions[i], Status: model.ShipSailing}
	}
}

func resetTestShares(g *model.Game) {
	for _, pid := range model.PlayerOrder {
		g.Players[pid].Shares = map[model.GoodsID]int{}
		for _, gid := range model.GoodsOrder {
			g.Players[pid].Shares[gid] = 0
		}
	}
}

func addPublicShareBuy(g *model.Game, playerID int, gid model.GoodsID) {
	g.Events = append(g.Events, model.Event{
		Type: "ShareBought",
		Data: map[string]interface{}{"playerId": playerID, "goodsId": gid},
	})
	g.Players[playerID].Shares[gid]++
	if g.Goods[gid].SharesRemaining > 0 {
		g.Goods[gid].SharesRemaining--
	}
}

func goodsIDsFromInterfaces(items []interface{}) []model.GoodsID {
	out := make([]model.GoodsID, 0, len(items))
	for _, item := range items {
		out = append(out, model.GoodsID(intFromPayload(item, 0)))
	}
	return out
}

func containsGoods(items []model.GoodsID, gid model.GoodsID) bool {
	for _, item := range items {
		if item == gid {
			return true
		}
	}
	return false
}
