package rules

import (
	"testing"

	"manila/internal/model"
)

func TestStartGameInitializesFourPlayersAndShares(t *testing.T) {
	e := NewEngine()
	g := NewGame("g", 1)
	if err := e.StartGame(g); err != nil {
		t.Fatal(err)
	}
	if g.Status != model.StatusInProgress || g.Phase != model.PhaseAuction {
		t.Fatalf("unexpected status/phase: %s/%s", g.Status, g.Phase)
	}
	for _, p := range g.Players {
		if p.Cash != 30 || p.AvailablePieces != 3 {
			t.Fatalf("bad player init: %+v", p)
		}
		total := 0
		for _, n := range p.Shares {
			total += n
		}
		if total != 2 {
			t.Fatalf("player %d got %d shares", p.ID, total)
		}
	}
}

func TestAuctionAllPassMakesPreviousHarborMasterFree(t *testing.T) {
	e := NewEngine()
	g := NewGame("g", 2)
	if err := e.StartGame(g); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{1, 2, 3, 4} {
		if err := e.ApplyAction(g, model.Action{PlayerID: pid, Type: model.ActionPassBid}); err != nil {
			t.Fatalf("pass %d: %v", pid, err)
		}
	}
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != 1 {
		t.Fatalf("expected player 1 harbor master, got phase %s hm %d", g.Phase, g.HarborMaster)
	}
	if g.Players[1].Cash != 30 {
		t.Fatalf("free harbor master should not pay, cash=%d", g.Players[1].Cash)
	}
}

func TestSetupRoundAndPlacementHasNoPass(t *testing.T) {
	e, g := readyForPlacement(t)
	acts, err := e.LegalActions(g, g.CurrentPlayer)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) == 0 {
		t.Fatal("expected placement actions")
	}
	for _, act := range acts {
		if act.Type != model.ActionPlaceAccomplice {
			t.Fatalf("placement returned non-place action: %s", act.Type)
		}
	}
}

func TestBankruptPlayerUsesOnlyStowawayAction(t *testing.T) {
	e, g := readyForPlacement(t)
	p := g.Players[g.CurrentPlayer]
	p.Cash = 0
	for _, gid := range model.GoodsOrder {
		p.Shares[gid] = 0
	}
	p.MortgagedShareCount = 0
	g.Board.Insurance = &model.Piece{PlayerID: 4}
	acts, err := e.LegalActions(g, g.CurrentPlayer)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 {
		t.Fatalf("expected one bankrupt action, got %d", len(acts))
	}
	if !p.BankruptThisRound {
		t.Fatal("player should become bankrupt")
	}
	if acts[0].Payload["targetId"] != int(model.GoodsGinseng) {
		t.Fatalf("expected lowest id ship, got %#v", acts[0].Payload["targetId"])
	}
}

func TestNoSlotStowawayDoesNotOccupyAndCopiesPayout(t *testing.T) {
	e, g := readyForPlacement(t)
	p := g.Players[g.CurrentPlayer]
	p.Cash = 1
	for _, gid := range model.GoodsOrder {
		p.Shares[gid] = 0
	}
	p.MortgagedShareCount = 0
	g.Board.Insurance = &model.Piece{PlayerID: 4}
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		for ship.NextBoardingIndex < len(model.BoardingCosts(gid)) {
			ship.Occupants = append(ship.Occupants, model.Piece{PlayerID: 2})
			ship.NextBoardingIndex++
		}
	}
	acts, err := e.LegalActions(g, g.CurrentPlayer)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Payload["occupiesSlot"] != false {
		t.Fatalf("expected no-slot stowaway action, got %#v", acts)
	}
	if err := e.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionPlaceAccomplice, Payload: acts[0].Payload}); err != nil {
		t.Fatal(err)
	}
	ship := g.Ships[model.GoodsGinseng]
	if len(ship.Stowaways) != 1 {
		t.Fatalf("expected stowaway, got %+v", ship.Stowaways)
	}
	if len(ship.Occupants) != len(model.BoardingCosts(model.GoodsGinseng)) {
		t.Fatal("stowaway should not occupy a ship slot")
	}
	ship.Status = model.ShipArrived
	g.Round.ArrivedOrder = []model.GoodsID{model.GoodsGinseng}
	before := g.Players[1].Cash
	e.settleRound(g)
	if g.Players[1].Cash-before != 6 {
		t.Fatalf("stowaway should receive normal share 6, got %d", g.Players[1].Cash-before)
	}
}

func TestPirateBoardingMarksPirateBoardedAndKeepsMateRole(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Board.Pirates = []model.Piece{{PlayerID: 1, Role: "captain"}, {PlayerID: 2, Role: "mate"}}
	g.Ships[model.GoodsGinseng].Position = 13
	g.Phase = model.PhasePirateBoarding
	g.Round.PendingPirateQueue = append([]model.Piece{}, g.Board.Pirates...)
	g.CurrentPlayer = 1
	if err := e.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionPirateBoard, Payload: map[string]interface{}{"shipId": 1}}); err != nil {
		t.Fatal(err)
	}
	if len(g.Board.Pirates) != 1 || g.Board.Pirates[0].PlayerID != 2 || g.Board.Pirates[0].Role != "mate" {
		t.Fatalf("mate should stay mate after captain boards, pirates=%+v", g.Board.Pirates)
	}
	if len(g.Board.BoardedPirates) != 1 || g.Board.BoardedPirates[0].PlayerID != 1 || g.Board.BoardedPirates[0].Role != "boardedCaptain" {
		t.Fatalf("boarded captain marker missing, boarded=%+v", g.Board.BoardedPirates)
	}
	if got := g.Ships[model.GoodsGinseng].Occupants[len(g.Ships[model.GoodsGinseng].Occupants)-1]; got.PlayerID != 1 || got.Role != "boardedPirate" {
		t.Fatalf("captain should be marked as boarded pirate on ship, got %+v", got)
	}
}

func TestPirateLootingUsesMateAsLeaderWhenCaptainHasBoarded(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Board.Pirates = []model.Piece{{PlayerID: 2, Role: "mate"}}
	g.Board.BoardedPirates = []model.Piece{{PlayerID: 1, Role: "boardedCaptain"}}
	g.Ships[model.GoodsGinseng].Position = 13
	g.Ships[model.GoodsSilk].Status = model.ShipDocked
	g.Ships[model.GoodsNutmeg].Status = model.ShipDocked
	g.Round.MovementStep = 3

	e.afterThirdMove(g)

	if g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 2 {
		t.Fatalf("mate should lead looting after captain boarded, phase=%s current=%d", g.Phase, g.CurrentPlayer)
	}
	if g.Board.Pirates[0].Role != "mate" {
		t.Fatalf("mate should keep mate role while leading looting, pirates=%+v", g.Board.Pirates)
	}
}

func TestPiratePlacementTreatsBoardedPiratesAsOccupiedSlots(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Board.Pirates = nil
	g.Board.BoardedPirates = []model.Piece{{PlayerID: 1, Role: "boardedCaptain"}}
	g.CurrentPlayer = 2
	g.Players[2].Cash = 5

	acts, err := e.LegalActions(g, 2)
	if err != nil {
		t.Fatal(err)
	}
	pirateActs := []model.LegalAction{}
	for _, act := range acts {
		if act.Type == model.ActionPlaceAccomplice && act.Payload["positionType"] == "pirate" {
			pirateActs = append(pirateActs, act)
		}
	}
	if len(pirateActs) != 1 || pirateActs[0].Payload["targetId"] != 2 {
		t.Fatalf("captain slot is occupied by boarded marker, expected only mate slot, got %+v", pirateActs)
	}

	g.Board.Pirates = []model.Piece{{PlayerID: 3, Role: "mate"}}
	acts, err = e.LegalActions(g, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, act := range acts {
		if act.Type == model.ActionPlaceAccomplice && act.Payload["positionType"] == "pirate" {
			t.Fatalf("both pirate slots should be occupied, got pirate action %+v", act)
		}
	}
}

func TestMovementAndPirateLootEventOrder(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Round.MovementStep = 0
	g.Ships[model.GoodsGinseng].Position = 13
	g.Ships[model.GoodsNutmeg].Status = model.ShipDocked
	g.Ships[model.GoodsSilk].Status = model.ShipDocked
	g.Phase = model.PhasePlacement
	g.CurrentPlayer = 1

	before := g.EventSeq
	e.rollAndMove(g)
	if g.Events[before].Type != "DiceRolled" || g.Events[before+1].Type != "ShipArrived" {
		t.Fatalf("expected dice before arrival, got %s then %s", g.Events[before].Type, g.Events[before+1].Type)
	}

	e, g = readyForPlacement(t)
	g.Board.Pirates = []model.Piece{{PlayerID: 1, Role: "captain"}}
	g.Ships[model.GoodsGinseng].Position = 13
	g.Phase = model.PhasePirateLooting
	g.Round.PendingLootShips = []model.GoodsID{model.GoodsGinseng}
	g.CurrentPlayer = 1
	beforeLoot := g.EventSeq
	if err := e.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 1, "destination": "port"}}); err != nil {
		t.Fatal(err)
	}
	if g.Events[beforeLoot].Type != "PiratesLootedShip" || g.Events[beforeLoot+1].Type != "ShipArrived" {
		t.Fatalf("expected looting before arrival, got %s then %s", g.Events[beforeLoot].Type, g.Events[beforeLoot+1].Type)
	}
}

func TestNavigatorMoveEventBeforeArrival(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Phase = model.PhaseNavigatorAction
	g.Round.NavigatorStep = "small"
	g.CurrentPlayer = 1
	g.Ships[model.GoodsGinseng].Position = 13
	g.Ships[model.GoodsNutmeg].Status = model.ShipDocked
	g.Ships[model.GoodsSilk].Status = model.ShipDocked

	before := g.EventSeq
	err := e.ApplyAction(g, model.Action{
		PlayerID: 1,
		Type:     model.ActionNavigatorMove,
		Payload:  map[string]interface{}{"moves": []interface{}{map[string]interface{}{"shipId": 1, "delta": 1}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Events[before].Type != "NavigatorMoved" || g.Events[before+1].Type != "ShipArrived" {
		t.Fatalf("expected navigator move before arrival, got %s then %s", g.Events[before].Type, g.Events[before+1].Type)
	}
}

func TestNavigatorOnlyMoveActionAndZeroMoveIsAllowed(t *testing.T) {
	e, g := readyForPlacement(t)
	g.Phase = model.PhaseNavigatorAction
	g.Round.NavigatorStep = "small"
	g.CurrentPlayer = 1

	acts, err := e.LegalActions(g, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(acts) != 1 || acts[0].Type != model.ActionNavigatorMove {
		t.Fatalf("navigator should only offer move action, got %+v", acts)
	}

	before := g.EventSeq
	err = e.ApplyAction(g, model.Action{
		PlayerID: 1,
		Type:     model.ActionNavigatorMove,
		Payload:  map[string]interface{}{"moves": []interface{}{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if g.Events[before].Type != "NavigatorMoved" {
		t.Fatalf("zero move should be recorded as NavigatorMoved, got %s", g.Events[before].Type)
	}
}

func TestNavigatorAllowsZeroEntriesForUnmovedShips(t *testing.T) {
	e, g := readyForPlacement(t)
	moves := []interface{}{
		map[string]interface{}{"shipId": 1, "delta": 1},
		map[string]interface{}{"shipId": 2, "delta": 0},
		map[string]interface{}{"shipId": 3, "delta": 0},
	}
	if _, err := e.applyNavigatorMoves(g, moves, "small"); err != nil {
		t.Fatalf("navigator should allow zero entries for unmoved ships: %v", err)
	}

	moves = []interface{}{
		map[string]interface{}{"shipId": 1, "delta": 1},
		map[string]interface{}{"shipId": 2, "delta": -1},
		map[string]interface{}{"shipId": 3, "delta": 0},
	}
	if _, err := e.applyNavigatorMoves(g, moves, "big"); err != nil {
		t.Fatalf("big navigator should validate total movement, not entry count: %v", err)
	}
}

func TestRoundReviewRequiresAllPlayersToConfirm(t *testing.T) {
	e, g := readyForPlacement(t)
	round := g.RoundNumber
	e.beginRoundReview(g)
	if g.Phase != model.PhaseRoundReview || g.CurrentPlayer != 1 {
		t.Fatalf("expected round review at player 1, got phase %s player %d", g.Phase, g.CurrentPlayer)
	}
	for _, pid := range []int{1, 2, 3} {
		acts, err := e.LegalActions(g, pid)
		if err != nil {
			t.Fatal(err)
		}
		if len(acts) != 1 || acts[0].Type != model.ActionConfirmRound {
			t.Fatalf("expected confirm action for player %d, got %+v", pid, acts)
		}
		if err := e.ApplyAction(g, model.Action{PlayerID: pid, Type: model.ActionConfirmRound}); err != nil {
			t.Fatal(err)
		}
		if g.Phase != model.PhaseRoundReview {
			t.Fatalf("should stay in review before all confirm, got %s", g.Phase)
		}
	}
	if err := e.ApplyAction(g, model.Action{PlayerID: 4, Type: model.ActionConfirmRound}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != model.PhaseAuction || g.RoundNumber != round+1 {
		t.Fatalf("expected next auction after all confirmations, got phase %s round %d", g.Phase, g.RoundNumber)
	}
}

func TestScriptedLegalActionsCanAdvanceManySteps(t *testing.T) {
	e := NewEngine()
	g := NewGame("sim", 9)
	if err := e.StartGame(g); err != nil {
		t.Fatal(err)
	}
	for step := 0; step < 300 && g.Status != model.StatusEnded; step++ {
		action, ok := scriptedAction(t, e, g)
		if !ok {
			t.Fatalf("no scripted action for phase %s current player %d", g.Phase, g.CurrentPlayer)
		}
		if err := e.ApplyAction(g, action); err != nil {
			t.Fatalf("step %d phase %s action %+v failed: %v", step, g.Phase, action, err)
		}
	}
	if g.EventSeq == 0 {
		t.Fatal("simulation produced no events")
	}
}

func scriptedAction(t *testing.T, e *Engine, g *model.Game) (model.Action, bool) {
	t.Helper()
	pid := g.CurrentPlayer
	switch g.Phase {
	case model.PhaseAuction:
		return model.Action{PlayerID: pid, Type: model.ActionPassBid}, true
	case model.PhaseHarborMasterBuyShare:
		return model.Action{PlayerID: pid, Type: model.ActionSkipBuyShare}, true
	case model.PhaseHarborMasterSelectGoods:
		return model.Action{PlayerID: pid, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}, true
	case model.PhaseHarborMasterSetShips:
		return model.Action{PlayerID: pid, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 3, "2": 3, "3": 3}}}, true
	case model.PhasePlacement, model.PhasePirateBoarding, model.PhaseNavigatorAction, model.PhasePirateLooting:
		acts, err := e.LegalActions(g, pid)
		if err != nil || len(acts) == 0 {
			t.Fatalf("legal actions failed: %v actions=%+v", err, acts)
		}
		chosen := acts[0]
		return model.Action{PlayerID: pid, Type: chosen.Type, Payload: chosen.Payload}, true
	case model.PhaseRoundReview:
		return model.Action{PlayerID: pid, Type: model.ActionConfirmRound}, true
	default:
		return model.Action{}, false
	}
}

func readyForPlacement(t *testing.T) (*Engine, *model.Game) {
	t.Helper()
	e := NewEngine()
	g := NewGame("g", 3)
	if err := e.StartGame(g); err != nil {
		t.Fatal(err)
	}
	for _, pid := range []int{1, 2, 3, 4} {
		if err := e.ApplyAction(g, model.Action{PlayerID: pid, Type: model.ActionPassBid}); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSkipBuyShare}); err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	if err := e.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 3, "2": 3, "3": 3}}}); err != nil {
		t.Fatal(err)
	}
	if g.Phase != model.PhasePlacement {
		t.Fatalf("expected placement phase, got %s", g.Phase)
	}
	return e, g
}
