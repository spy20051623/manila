package rules

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strconv"

	"manila/internal/model"
)

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func NewGame(id string, seed int64) *model.Game {
	g := &model.Game{
		ID:                   id,
		Status:               model.StatusNotStarted,
		Phase:                model.PhaseNotStarted,
		RoundNumber:          0,
		Players:              map[int]*model.Player{},
		Goods:                map[model.GoodsID]*model.Goods{},
		Ships:                map[model.GoodsID]*model.Ship{},
		PreviousHarborMaster: 1,
		Seed:                 seed,
		Events:               []model.Event{},
	}
	for _, gid := range model.GoodsOrder {
		g.Goods[gid] = &model.Goods{
			ID:               gid,
			Name:             model.GoodsName(gid),
			MarketValueIndex: 0,
			SharesRemaining:  5,
			ShipPayout:       model.ShipPayout(gid),
			BoardingCosts:    model.BoardingCosts(gid),
		}
	}
	for _, pid := range model.PlayerOrder {
		g.Players[pid] = &model.Player{
			ID:              pid,
			Cash:            30,
			Shares:          map[model.GoodsID]int{},
			AvailablePieces: 3,
		}
		for _, gid := range model.GoodsOrder {
			g.Players[pid].Shares[gid] = 0
		}
	}
	g.Board = newBoard()
	return g
}

func (e *Engine) StartGame(g *model.Game) error {
	if g.Status != model.StatusNotStarted {
		return fmt.Errorf("game is not startable")
	}
	deck := []model.GoodsID{}
	for _, gid := range model.GoodsOrder {
		for i := 0; i < 5; i++ {
			deck = append(deck, gid)
		}
	}
	r := rand.New(rand.NewSource(g.Seed))
	r.Shuffle(len(deck), func(i, j int) { deck[i], deck[j] = deck[j], deck[i] })
	idx := 0
	for _, pid := range model.PlayerOrder {
		for i := 0; i < 2; i++ {
			gid := deck[idx]
			idx++
			g.Players[pid].Shares[gid]++
			g.Goods[gid].SharesRemaining--
		}
	}
	g.RandomDraws = 1
	g.Status = model.StatusInProgress
	g.RoundNumber = 1
	e.beginAuction(g, 1)
	e.addEvent(g, "GameStarted", "game started", nil)
	return nil
}

func (e *Engine) LegalActions(g *model.Game, playerID int) ([]model.LegalAction, error) {
	if g.Status == model.StatusEnded {
		return nil, nil
	}
	if _, ok := g.Players[playerID]; !ok {
		return nil, fmt.Errorf("unknown player %d", playerID)
	}
	if g.Phase != model.PhaseRoundReview && e.actorForPhase(g) != playerID {
		return []model.LegalAction{}, nil
	}
	switch g.Phase {
	case model.PhaseAuction:
		return e.legalAuction(g, playerID), nil
	case model.PhaseHarborMasterBuyShare:
		return e.legalBuyShare(g, playerID), nil
	case model.PhaseHarborMasterSelectGoods:
		return []model.LegalAction{{Type: model.ActionSelectGoods, Description: "select 3 goods"}}, nil
	case model.PhaseHarborMasterSetShips:
		return []model.LegalAction{{Type: model.ActionSetShipStarts, Description: "set ship starts, sum must be 9"}}, nil
	case model.PhasePlacement:
		return e.legalPlacement(g, playerID)
	case model.PhasePirateBoarding:
		return e.legalPirateBoard(g, playerID), nil
	case model.PhaseNavigatorAction:
		return e.legalNavigator(g, playerID), nil
	case model.PhasePirateLooting:
		return e.legalPirateLooting(g, playerID), nil
	case model.PhaseRoundReview:
		return e.legalRoundReview(g, playerID), nil
	default:
		return []model.LegalAction{}, nil
	}
}

func (e *Engine) actorForPhase(g *model.Game) int {
	switch g.Phase {
	case model.PhaseHarborMasterBuyShare, model.PhaseHarborMasterSelectGoods, model.PhaseHarborMasterSetShips:
		return g.HarborMaster
	default:
		return g.CurrentPlayer
	}
}

func (e *Engine) ApplyAction(g *model.Game, a model.Action) error {
	if a.ExpectedEventSeq != nil && *a.ExpectedEventSeq != g.EventSeq {
		return fmt.Errorf("event sequence mismatch: expected %d got %d", *a.ExpectedEventSeq, g.EventSeq)
	}
	if g.Phase != model.PhaseRoundReview && e.actorForPhase(g) != a.PlayerID {
		return fmt.Errorf("not player %d turn", a.PlayerID)
	}
	if g.Status == model.StatusEnded {
		return fmt.Errorf("game has ended")
	}
	var err error
	switch g.Phase {
	case model.PhaseAuction:
		err = e.applyAuction(g, a)
	case model.PhaseHarborMasterBuyShare:
		err = e.applyBuyShare(g, a)
	case model.PhaseHarborMasterSelectGoods:
		err = e.applySelectGoods(g, a)
	case model.PhaseHarborMasterSetShips:
		err = e.applySetShipStarts(g, a)
	case model.PhasePlacement:
		err = e.applyPlacement(g, a)
	case model.PhasePirateBoarding:
		err = e.applyPirateBoard(g, a)
	case model.PhaseNavigatorAction:
		err = e.applyNavigator(g, a)
	case model.PhasePirateLooting:
		err = e.applyPirateLooting(g, a)
	case model.PhaseRoundReview:
		err = e.applyRoundReview(g, a)
	default:
		err = fmt.Errorf("phase %s does not accept actions", g.Phase)
	}
	if err != nil {
		return err
	}
	e.autoAdvance(g)
	return nil
}

func (e *Engine) beginAuction(g *model.Game, start int) {
	g.Phase = model.PhaseAuction
	g.CurrentPlayer = start
	g.Auction = model.Auction{
		StartPlayer:   start,
		ActivePlayer:  start,
		PassedPlayers: map[int]bool{},
		CurrentBid:    0,
		HighestBidder: 0,
		HasAnyBid:     false,
	}
	g.Round = model.RoundState{}
	g.Board = newBoard()
	g.Ships = map[model.GoodsID]*model.Ship{}
	for _, p := range g.Players {
		p.AvailablePieces = 3
		p.PlacedPieces = 0
		p.BankruptThisRound = false
	}
}

func newBoard() model.Board {
	return model.Board{
		Ports: map[string]*model.BoardSlot{
			"A": {ID: "A", Cost: 4, Reward: 6},
			"B": {ID: "B", Cost: 3, Reward: 8},
			"C": {ID: "C", Cost: 2, Reward: 15},
		},
		Docks: map[string]*model.BoardSlot{
			"A": {ID: "A", Cost: 4, Reward: 6},
			"B": {ID: "B", Cost: 3, Reward: 8},
			"C": {ID: "C", Cost: 2, Reward: 15},
		},
		Pirates: []model.Piece{},
	}
}

func (e *Engine) legalAuction(g *model.Game, playerID int) []model.LegalAction {
	max := e.maxPayable(g.Players[playerID])
	acts := []model.LegalAction{{Type: model.ActionPassBid, Description: "pass bid"}}
	minBid := g.Auction.CurrentBid + 1
	if minBid <= max {
		acts = append(acts, model.LegalAction{
			Type:        model.ActionBid,
			Payload:     map[string]interface{}{"minBid": minBid, "maxBid": max},
			Description: "bid higher than current bid",
		})
	}
	return acts
}

func (e *Engine) applyAuction(g *model.Game, a model.Action) error {
	switch a.Type {
	case model.ActionBid:
		amount, err := intPayload(a.Payload, "amount")
		if err != nil {
			return err
		}
		if amount <= g.Auction.CurrentBid {
			return fmt.Errorf("bid must exceed current bid")
		}
		if amount > e.maxPayable(g.Players[a.PlayerID]) {
			return fmt.Errorf("bid exceeds max payable")
		}
		g.Auction.CurrentBid = amount
		g.Auction.HighestBidder = a.PlayerID
		g.Auction.HasAnyBid = true
		e.addEvent(g, "BidPlaced", "", map[string]interface{}{"playerId": a.PlayerID, "amount": amount})
	case model.ActionPassBid:
		g.Auction.PassedPlayers[a.PlayerID] = true
		e.addEvent(g, "BidPassed", "", map[string]interface{}{"playerId": a.PlayerID})
	default:
		return fmt.Errorf("invalid auction action %s", a.Type)
	}
	e.advanceAuction(g)
	return nil
}

func (e *Engine) advanceAuction(g *model.Game) {
	if !g.Auction.HasAnyBid && len(g.Auction.PassedPlayers) == 4 {
		e.setHarborMaster(g, g.PreviousHarborMaster, 0)
		return
	}
	if g.Auction.HasAnyBid && len(g.Auction.PassedPlayers) >= 3 {
		e.setHarborMaster(g, g.Auction.HighestBidder, g.Auction.CurrentBid)
		return
	}
	next := e.nextPlayer(g.CurrentPlayer)
	for g.Auction.PassedPlayers[next] {
		next = e.nextPlayer(next)
	}
	g.CurrentPlayer = next
	g.Auction.ActivePlayer = next
}

func (e *Engine) setHarborMaster(g *model.Game, playerID int, price int) {
	if price > 0 {
		_ = e.pay(g, playerID, price, "harbor master bid")
	}
	g.HarborMaster = playerID
	g.PreviousHarborMaster = playerID
	g.Phase = model.PhaseHarborMasterBuyShare
	g.CurrentPlayer = playerID
	e.addEvent(g, "HarborMasterSet", "", map[string]interface{}{"playerId": playerID, "price": price})
}

func (e *Engine) legalBuyShare(g *model.Game, playerID int) []model.LegalAction {
	acts := []model.LegalAction{{Type: model.ActionSkipBuyShare, Description: "skip buying share"}}
	for _, gid := range model.GoodsOrder {
		gd := g.Goods[gid]
		price := gd.MarketValue()
		if price < 5 {
			price = 5
		}
		if gd.SharesRemaining > 0 && price <= e.maxPayable(g.Players[playerID]) {
			acts = append(acts, model.LegalAction{
				Type:        model.ActionBuyShare,
				Payload:     map[string]interface{}{"goodsId": int(gid)},
				Cost:        price,
				Description: "buy share",
			})
		}
	}
	return acts
}

func (e *Engine) applyBuyShare(g *model.Game, a model.Action) error {
	if a.PlayerID != g.HarborMaster {
		return fmt.Errorf("only harbor master can buy share")
	}
	switch a.Type {
	case model.ActionSkipBuyShare:
		e.addEvent(g, "ShareBuySkipped", "", map[string]interface{}{"playerId": a.PlayerID})
	case model.ActionBuyShare:
		gid, err := goodsPayload(a.Payload, "goodsId")
		if err != nil {
			return err
		}
		gd := g.Goods[gid]
		if gd.SharesRemaining <= 0 {
			return fmt.Errorf("no shares remaining")
		}
		price := gd.MarketValue()
		if price < 5 {
			price = 5
		}
		if err := e.pay(g, a.PlayerID, price, "buy share"); err != nil {
			return err
		}
		gd.SharesRemaining--
		g.Players[a.PlayerID].Shares[gid]++
		e.addEvent(g, "ShareBought", "", map[string]interface{}{"playerId": a.PlayerID, "goodsId": gid, "price": price})
	default:
		return fmt.Errorf("invalid buy share action")
	}
	g.Phase = model.PhaseHarborMasterSelectGoods
	return nil
}

func (e *Engine) applySelectGoods(g *model.Game, a model.Action) error {
	if a.Type != model.ActionSelectGoods || a.PlayerID != g.HarborMaster {
		return fmt.Errorf("invalid select goods action")
	}
	goods, err := goodsListPayload(a.Payload, "goodsIds")
	if err != nil {
		return err
	}
	if len(goods) != 3 {
		return fmt.Errorf("must select 3 goods")
	}
	seen := map[model.GoodsID]bool{}
	for _, gid := range goods {
		if seen[gid] {
			return fmt.Errorf("duplicate goods")
		}
		seen[gid] = true
	}
	var excluded model.GoodsID
	for _, gid := range model.GoodsOrder {
		if !seen[gid] {
			excluded = gid
		}
	}
	g.Round.SelectedGoods = append([]model.GoodsID{}, goods...)
	g.Round.ExcludedGoods = excluded
	g.Ships = map[model.GoodsID]*model.Ship{}
	for i, gid := range goods {
		g.Ships[gid] = &model.Ship{ID: gid, GoodsID: gid, RouteIndex: i + 1, Status: model.ShipSailing}
	}
	g.Phase = model.PhaseHarborMasterSetShips
	e.addEvent(g, "GoodsSelected", "", map[string]interface{}{"goodsIds": goods, "excludedGoodsId": excluded})
	return nil
}

func (e *Engine) applySetShipStarts(g *model.Game, a model.Action) error {
	if a.Type != model.ActionSetShipStarts || a.PlayerID != g.HarborMaster {
		return fmt.Errorf("invalid set ship starts action")
	}
	startsRaw, ok := a.Payload["starts"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("payload.starts required")
	}
	sum := 0
	for _, gid := range g.Round.SelectedGoods {
		v, ok := startsRaw[strconv.Itoa(int(gid))]
		if !ok {
			return fmt.Errorf("missing start for goods %d", gid)
		}
		n, err := toInt(v)
		if err != nil {
			return err
		}
		if n < 0 || n > 5 {
			return fmt.Errorf("ship start must be 0..5")
		}
		g.Ships[gid].Position = n
		sum += n
	}
	if sum != 9 {
		return fmt.Errorf("ship starts must sum to 9")
	}
	g.Round.PlacementStep = 1
	g.Round.PlacementTurnsTaken = 0
	g.Phase = model.PhasePlacement
	g.CurrentPlayer = g.HarborMaster
	e.addEvent(g, "ShipStartsSet", "", map[string]interface{}{"starts": startsRaw})
	return nil
}

func (e *Engine) legalPlacement(g *model.Game, playerID int) ([]model.LegalAction, error) {
	p := g.Players[playerID]
	if p.AvailablePieces <= 0 {
		return nil, fmt.Errorf("player has no pieces")
	}
	e.ensureBankruptState(g, playerID)
	if p.BankruptThisRound {
		return []model.LegalAction{e.bankruptPlacementAction(g, playerID)}, nil
	}
	acts := []model.LegalAction{}
	max := e.maxPayable(p)
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		costs := model.BoardingCosts(gid)
		if ship.Status == model.ShipSailing && ship.NextBoardingIndex < len(costs) {
			cost := costs[ship.NextBoardingIndex]
			if cost <= max {
				acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": int(gid)}, Cost: cost, Description: "place on ship"})
			}
		}
	}
	for _, id := range []string{"A", "B", "C"} {
		slot := g.Board.Ports[id]
		if slot.Occupant == nil && slot.Cost <= max {
			acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": id}, Cost: slot.Cost, Description: "place on port"})
		}
	}
	for _, id := range []string{"A", "B", "C"} {
		slot := g.Board.Docks[id]
		if slot.Occupant == nil && slot.Cost <= max {
			acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": id}, Cost: slot.Cost, Description: "place on dock"})
		}
	}
	if targetID, ok := e.nextPiratePlacementSlot(g); ok && 5 <= max {
		acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": targetID}, Cost: 5, Description: "place on pirate ship"})
	}
	if g.Board.SmallNavigator == nil && 2 <= max {
		acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorSmall", "targetId": "small"}, Cost: 2, Description: "place small navigator"})
	}
	if g.Board.BigNavigator == nil && 5 <= max {
		acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorBig", "targetId": "big"}, Cost: 5, Description: "place big navigator"})
	}
	if g.Board.Insurance == nil {
		acts = append(acts, model.LegalAction{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}, Cost: 0, Description: "place insurance"})
	}
	if len(acts) == 0 {
		return nil, fmt.Errorf("non-bankrupt player has no legal placement")
	}
	return acts, nil
}

func (e *Engine) ensureBankruptState(g *model.Game, playerID int) {
	p := g.Players[playerID]
	if p.BankruptThisRound {
		return
	}
	if e.unmortgagedShares(p) > 0 {
		return
	}
	minCost, ok := e.minNormalPlacementCost(g)
	if !ok {
		return
	}
	if p.Cash < minCost {
		p.BankruptThisRound = true
		e.addEvent(g, "PlayerBecameBankrupt", "", map[string]interface{}{"playerId": playerID})
	}
}

func (e *Engine) minNormalPlacementCost(g *model.Game) (int, bool) {
	min := 0
	found := false
	add := func(cost int) {
		if !found || cost < min {
			min = cost
			found = true
		}
	}
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		costs := model.BoardingCosts(gid)
		if ship.Status == model.ShipSailing && ship.NextBoardingIndex < len(costs) {
			add(costs[ship.NextBoardingIndex])
		}
	}
	for _, slot := range g.Board.Ports {
		if slot.Occupant == nil {
			add(slot.Cost)
		}
	}
	for _, slot := range g.Board.Docks {
		if slot.Occupant == nil {
			add(slot.Cost)
		}
	}
	if _, ok := e.nextPiratePlacementSlot(g); ok {
		add(5)
	}
	if g.Board.SmallNavigator == nil {
		add(2)
	}
	if g.Board.BigNavigator == nil {
		add(5)
	}
	if g.Board.Insurance == nil {
		add(0)
	}
	return min, found
}

func (e *Engine) bankruptPlacementAction(g *model.Game, playerID int) model.LegalAction {
	gid, occupies := e.bankruptTarget(g)
	return model.LegalAction{
		Type: model.ActionPlaceAccomplice,
		Payload: map[string]interface{}{
			"positionType": "ship",
			"targetId":     int(gid),
			"isStowaway":   true,
			"occupiesSlot": occupies,
		},
		Cost:        g.Players[playerID].Cash,
		Description: "stowaway placement",
	}
}

func (e *Engine) bankruptTarget(g *model.Game) (model.GoodsID, bool) {
	bestCost := 0
	best := model.GoodsID(0)
	found := false
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		costs := model.BoardingCosts(gid)
		if ship.Status == model.ShipSailing && ship.NextBoardingIndex < len(costs) {
			cost := costs[ship.NextBoardingIndex]
			if !found || cost < bestCost || (cost == bestCost && gid < best) {
				found = true
				bestCost = cost
				best = gid
			}
		}
	}
	if found {
		return best, true
	}
	selected := sortedSelected(g)
	if len(selected) == 0 {
		return 0, false
	}
	return selected[0], false
}

func (e *Engine) applyPlacement(g *model.Game, a model.Action) error {
	if a.Type != model.ActionPlaceAccomplice {
		return fmt.Errorf("placement requires PlaceAccomplice")
	}
	legal, err := e.legalPlacement(g, a.PlayerID)
	if err != nil {
		return err
	}
	if len(legal) == 0 {
		return fmt.Errorf("no legal placement")
	}
	pos, _ := stringPayload(a.Payload, "positionType")
	target := a.Payload["targetId"]
	var chosen *model.LegalAction
	for i := range legal {
		lp := legal[i].Payload
		if lp["positionType"] == pos && fmt.Sprint(lp["targetId"]) == fmt.Sprint(target) {
			chosen = &legal[i]
			break
		}
	}
	if chosen == nil {
		return fmt.Errorf("illegal placement target")
	}
	piece := model.Piece{PlayerID: a.PlayerID}
	cost := chosen.Cost
	if err := e.pay(g, a.PlayerID, cost, "place accomplice"); err != nil {
		return err
	}
	switch pos {
	case "ship":
		gid, err := goodsFromAny(target)
		if err != nil {
			return err
		}
		occupies := true
		if v, ok := chosen.Payload["occupiesSlot"].(bool); ok {
			occupies = v
		}
		slot := len(g.Ships[gid].Occupants)
		stowawaySlot := len(g.Ships[gid].Stowaways)
		if occupies {
			g.Ships[gid].Occupants = append(g.Ships[gid].Occupants, piece)
			g.Ships[gid].NextBoardingIndex++
		} else {
			g.Ships[gid].Stowaways = append(g.Ships[gid].Stowaways, piece)
		}
		data := map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "shipId": gid, "stowawaySlot": stowawaySlot, "cost": cost, "stowaway": !occupies}
		if occupies {
			data["slot"] = slot
			data["boardingSlot"] = slot + 1
		}
		e.addEvent(g, "AccomplicePlaced", "", data)
	case "port":
		id := fmt.Sprint(target)
		g.Board.Ports[id].Occupant = &piece
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": id, "cost": cost})
	case "dock":
		id := fmt.Sprint(target)
		g.Board.Docks[id].Occupant = &piece
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": id, "cost": cost})
	case "pirate":
		targetID, _ := toInt(target)
		if targetID == 1 {
			piece.Role = "captain"
		} else if targetID == 2 {
			piece.Role = "mate"
		} else {
			return fmt.Errorf("invalid pirate slot")
		}
		g.Board.Pirates = append(g.Board.Pirates, piece)
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": targetID, "cost": cost})
	case "navigatorSmall":
		g.Board.SmallNavigator = &piece
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": "small", "cost": cost})
	case "navigatorBig":
		g.Board.BigNavigator = &piece
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": "big", "cost": cost})
	case "insurance":
		g.Board.Insurance = &piece
		g.Players[a.PlayerID].Cash += 10
		e.addEvent(g, "AccomplicePlaced", "", map[string]interface{}{"playerId": a.PlayerID, "positionType": pos, "slot": "insurance", "reward": 10})
	default:
		return fmt.Errorf("unknown position type %s", pos)
	}
	g.Players[a.PlayerID].AvailablePieces--
	g.Players[a.PlayerID].PlacedPieces++
	g.Round.PlacementTurnsTaken++
	if g.Round.PlacementTurnsTaken >= 4 {
		g.Round.PlacementTurnsTaken = 0
		if g.Round.PlacementStep == 3 {
			e.afterPlacementStep3(g)
		} else {
			e.rollAndMove(g)
		}
	} else {
		g.CurrentPlayer = e.nextPlayer(g.CurrentPlayer)
	}
	return nil
}

func (e *Engine) afterPlacementStep3(g *model.Game) {
	if g.Board.SmallNavigator != nil {
		g.Phase = model.PhaseNavigatorAction
		g.Round.NavigatorStep = "small"
		g.CurrentPlayer = g.Board.SmallNavigator.PlayerID
		return
	}
	if g.Board.BigNavigator != nil {
		g.Phase = model.PhaseNavigatorAction
		g.Round.NavigatorStep = "big"
		g.CurrentPlayer = g.Board.BigNavigator.PlayerID
		return
	}
	e.rollAndMove(g)
}

func (e *Engine) rollAndMove(g *model.Game) {
	g.Round.MovementStep++
	step := g.Round.MovementStep
	results := map[model.GoodsID]int{}
	arrivedAfterRoll := []model.GoodsID{}
	r := rand.New(rand.NewSource(g.Seed + int64(g.RandomDraws)*7919))
	g.RandomDraws++
	for _, gid := range g.Round.SelectedGoods {
		ship := g.Ships[gid]
		if ship.Status != model.ShipSailing {
			continue
		}
		roll := r.Intn(6) + 1
		results[gid] = roll
		ship.Position += roll
		if ship.Position > 13 {
			arrivedAfterRoll = append(arrivedAfterRoll, gid)
		}
	}
	g.Round.DiceResults = append(g.Round.DiceResults, model.DiceRoll{MovementStep: step, Results: results})
	e.addEvent(g, "DiceRolled", "", map[string]interface{}{"movementStep": step, "results": results})
	for _, gid := range arrivedAfterRoll {
		e.markArrived(g, gid)
	}
	switch step {
	case 1:
		g.Round.PlacementStep = 2
		g.Phase = model.PhasePlacement
		g.CurrentPlayer = g.HarborMaster
	case 2:
		e.startPirateBoardingOrPlacement3(g)
	case 3:
		e.afterThirdMove(g)
	}
}

func (e *Engine) startPirateBoardingOrPlacement3(g *model.Game) {
	candidates := e.pirateBoardCandidates(g)
	if len(candidates) > 0 && len(g.Board.Pirates) > 0 {
		g.Phase = model.PhasePirateBoarding
		g.Round.PendingPirateQueue = append([]model.Piece{}, g.Board.Pirates...)
		g.CurrentPlayer = g.Round.PendingPirateQueue[0].PlayerID
		return
	}
	g.Round.PlacementStep = 3
	g.Phase = model.PhasePlacement
	g.CurrentPlayer = g.HarborMaster
}

func (e *Engine) pirateBoardCandidates(g *model.Game) []model.GoodsID {
	out := []model.GoodsID{}
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		if ship.Status == model.ShipSailing && ship.Position == 13 && ship.NextBoardingIndex < len(model.BoardingCosts(gid)) {
			out = append(out, gid)
		}
	}
	return out
}

func (e *Engine) legalPirateBoard(g *model.Game, playerID int) []model.LegalAction {
	if len(g.Round.PendingPirateQueue) == 0 || g.Round.PendingPirateQueue[0].PlayerID != playerID {
		return []model.LegalAction{}
	}
	acts := []model.LegalAction{{Type: model.ActionPirateSkipBoard, Description: "skip pirate boarding"}}
	for _, gid := range e.pirateBoardCandidates(g) {
		acts = append(acts, model.LegalAction{Type: model.ActionPirateBoard, Payload: map[string]interface{}{"shipId": int(gid)}, Description: "board ship"})
	}
	return acts
}

func (e *Engine) applyPirateBoard(g *model.Game, a model.Action) error {
	if len(g.Round.PendingPirateQueue) == 0 || g.Round.PendingPirateQueue[0].PlayerID != a.PlayerID {
		return fmt.Errorf("not current pirate")
	}
	pirate := g.Round.PendingPirateQueue[0]
	g.Round.PendingPirateQueue = g.Round.PendingPirateQueue[1:]
	if a.Type == model.ActionPirateBoard {
		gid, err := goodsPayload(a.Payload, "shipId")
		if err != nil {
			return err
		}
		valid := false
		for _, c := range e.pirateBoardCandidates(g) {
			if c == gid {
				valid = true
			}
		}
		if !valid {
			return fmt.Errorf("ship is not a valid pirate boarding target")
		}
		originalRole := pirate.Role
		e.removePirate(g, pirate.PlayerID)
		boardedMarker := pirate
		boardedMarker.Role = "boardedCaptain"
		if originalRole == "mate" {
			boardedMarker.Role = "boardedMate"
		}
		g.Board.BoardedPirates = append(g.Board.BoardedPirates, boardedMarker)
		pirate.Role = "boardedPirate"
		slot := len(g.Ships[gid].Occupants)
		g.Ships[gid].Occupants = append(g.Ships[gid].Occupants, pirate)
		g.Ships[gid].NextBoardingIndex++
		e.addEvent(g, "PirateBoarded", "", map[string]interface{}{"playerId": a.PlayerID, "shipId": gid, "slot": slot, "boardingSlot": slot + 1})
	} else if a.Type == model.ActionPirateSkipBoard {
		e.addEvent(g, "PirateBoardSkipped", "", map[string]interface{}{"playerId": a.PlayerID})
	} else {
		return fmt.Errorf("invalid pirate boarding action")
	}
	if len(g.Round.PendingPirateQueue) > 0 {
		g.CurrentPlayer = g.Round.PendingPirateQueue[0].PlayerID
	} else {
		g.Round.PlacementStep = 3
		g.Phase = model.PhasePlacement
		g.CurrentPlayer = g.HarborMaster
	}
	return nil
}

func (e *Engine) legalNavigator(g *model.Game, playerID int) []model.LegalAction {
	return []model.LegalAction{
		{Type: model.ActionNavigatorMove, Description: "move ship(s)"},
	}
}

func (e *Engine) applyNavigator(g *model.Game, a model.Action) error {
	if a.Type == model.ActionNavigatorMove {
		movesRaw, ok := a.Payload["moves"].([]interface{})
		if !ok {
			return fmt.Errorf("payload.moves required")
		}
		arrived, err := e.applyNavigatorMoves(g, movesRaw, g.Round.NavigatorStep)
		if err != nil {
			return err
		}
		e.addEvent(g, "NavigatorMoved", "", map[string]interface{}{"playerId": a.PlayerID, "moves": movesRaw, "navigator": g.Round.NavigatorStep})
		for _, gid := range arrived {
			e.markArrived(g, gid)
		}
	} else {
		return fmt.Errorf("invalid navigator action")
	}
	if g.Round.NavigatorStep == "small" && g.Board.BigNavigator != nil {
		g.Round.NavigatorStep = "big"
		g.CurrentPlayer = g.Board.BigNavigator.PlayerID
		return nil
	}
	g.Round.NavigatorStep = ""
	e.rollAndMove(g)
	return nil
}

func (e *Engine) applyNavigatorMoves(g *model.Game, moves []interface{}, step string) ([]model.GoodsID, error) {
	totalAbs := 0
	seen := map[model.GoodsID]bool{}
	for _, mv := range moves {
		m, ok := mv.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("invalid move")
		}
		gid, err := goodsFromAny(m["shipId"])
		if err != nil {
			return nil, err
		}
		delta, err := toInt(m["delta"])
		if err != nil {
			return nil, err
		}
		if delta == 0 {
			continue
		}
		if seen[gid] {
			return nil, fmt.Errorf("ship moved twice")
		}
		seen[gid] = true
		if step == "small" && (delta < -1 || delta > 1) {
			return nil, fmt.Errorf("small navigator delta must be -1..1")
		}
		if step == "big" && (delta < -2 || delta > 2) {
			return nil, fmt.Errorf("big navigator delta must be -2..2")
		}
		if delta < 0 {
			totalAbs -= delta
		} else {
			totalAbs += delta
		}
		ship := g.Ships[gid]
		if ship == nil || ship.Status != model.ShipSailing {
			return nil, fmt.Errorf("ship cannot be moved")
		}
	}
	if step == "small" && totalAbs > 1 {
		return nil, fmt.Errorf("small navigator total movement exceeds 1")
	}
	if step == "big" && totalAbs > 2 {
		return nil, fmt.Errorf("big navigator total movement exceeds 2")
	}
	arrived := []model.GoodsID{}
	for _, mv := range moves {
		m := mv.(map[string]interface{})
		gid, _ := goodsFromAny(m["shipId"])
		delta, _ := toInt(m["delta"])
		ship := g.Ships[gid]
		ship.Position += delta
		if ship.Position < 0 {
			ship.Position = 0
		}
		if ship.Position > 13 {
			arrived = append(arrived, gid)
		}
	}
	return arrived, nil
}

func (e *Engine) afterThirdMove(g *model.Game) {
	targets := []model.GoodsID{}
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		if ship.Status == model.ShipSailing && ship.Position == 13 {
			targets = append(targets, gid)
		}
	}
	if leader := e.activePirateLeader(g); len(targets) > 0 && leader != nil {
		g.Phase = model.PhasePirateLooting
		g.Round.PendingLootShips = targets
		g.CurrentPlayer = leader.PlayerID
		return
	}
	for _, gid := range targets {
		e.markArrived(g, gid)
	}
	e.dockRemainingAndSettle(g)
}

func (e *Engine) legalPirateLooting(g *model.Game, playerID int) []model.LegalAction {
	leader := e.activePirateLeader(g)
	if leader == nil || leader.PlayerID != playerID || len(g.Round.PendingLootShips) == 0 {
		return []model.LegalAction{}
	}
	gid := g.Round.PendingLootShips[0]
	return []model.LegalAction{
		{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": int(gid), "destination": "port"}, Description: "send looted ship to port"},
		{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": int(gid), "destination": "dock"}, Description: "send looted ship to dock"},
	}
}

func (e *Engine) applyPirateLooting(g *model.Game, a model.Action) error {
	if a.Type != model.ActionPirateChooseDestination {
		return fmt.Errorf("invalid pirate looting action")
	}
	leader := e.activePirateLeader(g)
	if leader == nil || leader.PlayerID != a.PlayerID {
		return fmt.Errorf("not pirate captain")
	}
	if len(g.Round.PendingLootShips) == 0 {
		return fmt.Errorf("no pending looted ships")
	}
	gid := g.Round.PendingLootShips[0]
	payloadGID, err := goodsPayload(a.Payload, "shipId")
	if err != nil {
		return err
	}
	if payloadGID != gid {
		return fmt.Errorf("must resolve first pending looted ship")
	}
	dest, err := stringPayload(a.Payload, "destination")
	if err != nil {
		return err
	}
	ship := g.Ships[gid]
	ship.LootedByPirates = true
	e.addEvent(g, "PiratesLootedShip", "", map[string]interface{}{"shipId": gid, "destination": dest})
	if dest == "port" {
		e.markArrived(g, gid)
	} else if dest == "dock" {
		e.markDocked(g, gid)
	} else {
		return fmt.Errorf("destination must be port or dock")
	}
	g.Round.PendingLootShips = g.Round.PendingLootShips[1:]
	if len(g.Round.PendingLootShips) > 0 {
		g.CurrentPlayer = leader.PlayerID
	} else {
		e.dockRemainingAndSettle(g)
	}
	return nil
}

func (e *Engine) dockRemainingAndSettle(g *model.Game) {
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		if ship.Status == model.ShipSailing {
			if ship.Position == 13 && e.activePirateLeader(g) == nil {
				e.markArrived(g, gid)
			} else {
				e.markDocked(g, gid)
			}
		}
	}
	e.settleRound(g)
}

func (e *Engine) settleRound(g *model.Game) {
	// Pirate loot payouts.
	looted := []model.GoodsID{}
	for _, gid := range sortedSelected(g) {
		if g.Ships[gid].LootedByPirates {
			looted = append(looted, gid)
		}
	}
	for _, gid := range looted {
		if e.activePirateLeader(g) != nil && len(g.Board.Pirates) > 0 {
			share := model.ShipPayout(gid) / len(g.Board.Pirates)
			for _, pirate := range g.Board.Pirates {
				g.Players[pirate.PlayerID].Cash += share
				e.addEvent(g, "PlayerReceivedPayout", "", map[string]interface{}{"playerId": pirate.PlayerID, "amount": share, "source": "pirates", "shipId": gid})
			}
		}
	}
	// Normal ship payouts.
	for _, gid := range sortedSelected(g) {
		ship := g.Ships[gid]
		if ship.Status == model.ShipArrived && !ship.LootedByPirates && len(ship.Occupants) > 0 {
			share := model.ShipPayout(gid) / len(ship.Occupants)
			for _, occ := range ship.Occupants {
				g.Players[occ.PlayerID].Cash += share
				e.addEvent(g, "PlayerReceivedPayout", "", map[string]interface{}{"playerId": occ.PlayerID, "amount": share, "source": "ship", "shipId": gid})
			}
			for _, st := range ship.Stowaways {
				g.Players[st.PlayerID].Cash += share
				e.addEvent(g, "StowawayPayoutReceived", "", map[string]interface{}{"playerId": st.PlayerID, "amount": share, "shipId": gid})
			}
		} else if len(ship.Stowaways) > 0 && ship.Status == model.ShipArrived && !ship.LootedByPirates {
			e.addEvent(g, "RuleError", "stowaways without normal occupants", map[string]interface{}{"shipId": gid})
		}
	}
	// Port payouts.
	for _, slotID := range []string{"A", "B", "C"} {
		slot := g.Board.Ports[slotID]
		if slot.Occupant != nil && e.shipInPortSlot(g, slotID) {
			g.Players[slot.Occupant.PlayerID].Cash += slot.Reward
			e.addEvent(g, "PlayerReceivedPayout", "", map[string]interface{}{"playerId": slot.Occupant.PlayerID, "amount": slot.Reward, "source": "port", "slot": slotID})
		}
	}
	// Dock payouts, with insurance if present.
	insuranceID := 0
	if g.Board.Insurance != nil {
		insuranceID = g.Board.Insurance.PlayerID
	}
	for _, slotID := range []string{"A", "B", "C"} {
		slot := g.Board.Docks[slotID]
		if slot.Occupant != nil && e.shipInDockSlot(g, slotID) {
			if insuranceID != 0 {
				e.payInsurance(g, insuranceID, slot.Reward)
			}
			g.Players[slot.Occupant.PlayerID].Cash += slot.Reward
			e.addEvent(g, "PlayerReceivedPayout", "", map[string]interface{}{"playerId": slot.Occupant.PlayerID, "amount": slot.Reward, "source": "dock", "slot": slotID})
		}
	}
	// Market advance.
	gameEnds := false
	for _, gid := range g.Round.ArrivedOrder {
		gd := g.Goods[gid]
		if gd.MarketValueIndex < len(model.MarketValues)-1 {
			gd.MarketValueIndex++
			e.addEvent(g, "GoodsMarketAdvanced", "", map[string]interface{}{"goodsId": gid, "marketValue": gd.MarketValue()})
		}
		if gd.MarketValueIndex == len(model.MarketValues)-1 {
			gameEnds = true
		}
	}
	if gameEnds {
		e.endGame(g)
		return
	}
	e.beginRoundReview(g)
}

func (e *Engine) beginRoundReview(g *model.Game) {
	g.Phase = model.PhaseRoundReview
	g.CurrentPlayer = 1
	g.Round.ConfirmedPlayers = map[int]bool{}
	e.addEvent(g, "RoundReviewStarted", "", map[string]interface{}{"roundNumber": g.RoundNumber})
}

func (e *Engine) legalRoundReview(g *model.Game, playerID int) []model.LegalAction {
	if g.Round.ConfirmedPlayers != nil && g.Round.ConfirmedPlayers[playerID] {
		return []model.LegalAction{}
	}
	return []model.LegalAction{{Type: model.ActionConfirmRound, Description: "confirm round result"}}
}

func (e *Engine) applyRoundReview(g *model.Game, a model.Action) error {
	if a.Type != model.ActionConfirmRound {
		return fmt.Errorf("round review requires ConfirmRound")
	}
	if g.Round.ConfirmedPlayers == nil {
		g.Round.ConfirmedPlayers = map[int]bool{}
	}
	if g.Round.ConfirmedPlayers[a.PlayerID] {
		return fmt.Errorf("player %d already confirmed round", a.PlayerID)
	}
	g.Round.ConfirmedPlayers[a.PlayerID] = true
	e.addEvent(g, "RoundConfirmed", "", map[string]interface{}{"playerId": a.PlayerID, "roundNumber": g.RoundNumber})
	for _, pid := range model.PlayerOrder {
		if !g.Round.ConfirmedPlayers[pid] {
			g.CurrentPlayer = pid
			return nil
		}
	}
	g.RoundNumber++
	e.beginAuction(g, g.PreviousHarborMaster)
	e.addEvent(g, "RoundStarted", "", map[string]interface{}{"roundNumber": g.RoundNumber, "startPlayer": g.Auction.StartPlayer})
	return nil
}

func (e *Engine) endGame(g *model.Game) {
	g.Status = model.StatusEnded
	g.Phase = model.PhaseGameEnd
	g.CurrentPlayer = 0
	scores := []model.Score{}
	for _, pid := range model.PlayerOrder {
		p := g.Players[pid]
		wealth := p.Cash
		for _, gid := range model.GoodsOrder {
			wealth += p.Shares[gid] * g.Goods[gid].MarketValue()
		}
		wealth -= p.MortgagedShareCount * 15
		scores = append(scores, model.Score{PlayerID: pid, Wealth: wealth})
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].Wealth == scores[j].Wealth {
			return scores[i].PlayerID < scores[j].PlayerID
		}
		return scores[i].Wealth > scores[j].Wealth
	})
	rank := 1
	for i := range scores {
		if i > 0 && scores[i].Wealth < scores[i-1].Wealth {
			rank = i + 1
		}
		scores[i].Rank = rank
	}
	g.FinalScores = scores
	e.addEvent(g, "GameEnded", "", map[string]interface{}{"scores": scores})
}

func (e *Engine) autoAdvance(g *model.Game) {}

func (e *Engine) markArrived(g *model.Game, gid model.GoodsID) {
	ship := g.Ships[gid]
	if ship.Status == model.ShipArrived {
		return
	}
	ship.Status = model.ShipArrived
	slot := string(rune('A' + len(g.Round.ArrivedOrder)))
	ship.PortSlot = slot
	g.Round.ArrivedOrder = append(g.Round.ArrivedOrder, gid)
	e.addEvent(g, "ShipArrived", "", map[string]interface{}{"shipId": gid, "slot": slot})
}

func (e *Engine) markDocked(g *model.Game, gid model.GoodsID) {
	ship := g.Ships[gid]
	if ship.Status == model.ShipDocked {
		return
	}
	ship.Status = model.ShipDocked
	slot := string(rune('A' + len(g.Round.DockedOrder)))
	ship.DockSlot = slot
	g.Round.DockedOrder = append(g.Round.DockedOrder, gid)
	e.addEvent(g, "ShipDocked", "", map[string]interface{}{"shipId": gid, "slot": slot})
}

func (e *Engine) shipInPortSlot(g *model.Game, slot string) bool {
	for _, ship := range g.Ships {
		if ship.PortSlot == slot {
			return true
		}
	}
	return false
}

func (e *Engine) shipInDockSlot(g *model.Game, slot string) bool {
	for _, ship := range g.Ships {
		if ship.DockSlot == slot {
			return true
		}
	}
	return false
}

func (e *Engine) payInsurance(g *model.Game, playerID int, amount int) {
	p := g.Players[playerID]
	needed := amount - p.Cash
	if needed > 0 {
		e.autoMortgage(g, playerID, needed)
	}
	paid := amount
	if p.Cash < amount {
		paid = p.Cash
	}
	p.Cash -= paid
	if paid < amount {
		e.addEvent(g, "BankCoveredInsuranceShortfall", "", map[string]interface{}{"playerId": playerID, "amount": amount - paid})
	}
	e.addEvent(g, "InsurancePaidDockReward", "", map[string]interface{}{"playerId": playerID, "amount": paid})
}

func (e *Engine) pay(g *model.Game, playerID int, amount int, reason string) error {
	p := g.Players[playerID]
	if amount <= 0 {
		return nil
	}
	if p.Cash < amount {
		e.autoMortgage(g, playerID, amount-p.Cash)
	}
	if p.Cash < amount {
		return fmt.Errorf("player %d cannot pay %d", playerID, amount)
	}
	p.Cash -= amount
	e.addEvent(g, "PlayerPaid", "", map[string]interface{}{"playerId": playerID, "amount": amount, "reason": reason})
	return nil
}

func (e *Engine) autoMortgage(g *model.Game, playerID int, needed int) {
	p := g.Players[playerID]
	for needed > 0 && e.unmortgagedShares(p) > 0 {
		p.MortgagedShareCount++
		p.Cash += 12
		needed -= 12
		e.addEvent(g, "ShareMortgaged", "", map[string]interface{}{"playerId": playerID, "mortgagedShareCount": p.MortgagedShareCount, "amount": 12})
	}
}

func (e *Engine) maxPayable(p *model.Player) int {
	return p.Cash + e.unmortgagedShares(p)*12
}

func (e *Engine) unmortgagedShares(p *model.Player) int {
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

func (e *Engine) addEvent(g *model.Game, typ, msg string, data map[string]interface{}) {
	g.EventSeq++
	g.Events = append(g.Events, model.Event{Seq: g.EventSeq, Type: typ, Message: msg, Data: data})
}

func (e *Engine) nextPlayer(playerID int) int {
	if playerID == 4 {
		return 1
	}
	return playerID + 1
}

func sortedSelected(g *model.Game) []model.GoodsID {
	out := append([]model.GoodsID{}, g.Round.SelectedGoods...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (e *Engine) removePirate(g *model.Game, playerID int) {
	next := []model.Piece{}
	for _, p := range g.Board.Pirates {
		if p.PlayerID != playerID {
			next = append(next, p)
		}
	}
	g.Board.Pirates = next
}

func (e *Engine) activePirateLeader(g *model.Game) *model.Piece {
	for i := range g.Board.Pirates {
		if g.Board.Pirates[i].Role == "captain" {
			return &g.Board.Pirates[i]
		}
	}
	for i := range g.Board.Pirates {
		if g.Board.Pirates[i].Role == "mate" {
			return &g.Board.Pirates[i]
		}
	}
	return nil
}

func (e *Engine) nextPiratePlacementSlot(g *model.Game) (int, bool) {
	if !e.pirateSlotOccupied(g, "captain") {
		return 1, true
	}
	if !e.pirateSlotOccupied(g, "mate") {
		return 2, true
	}
	return 0, false
}

func (e *Engine) pirateSlotOccupied(g *model.Game, role string) bool {
	boardedRole := "boardedCaptain"
	if role == "mate" {
		boardedRole = "boardedMate"
	}
	for _, pirate := range g.Board.Pirates {
		if pirate.Role == role {
			return true
		}
	}
	for _, pirate := range g.Board.BoardedPirates {
		if pirate.Role == boardedRole {
			return true
		}
	}
	return false
}

func intPayload(payload map[string]interface{}, key string) (int, error) {
	v, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("payload.%s required", key)
	}
	return toInt(v)
}

func stringPayload(payload map[string]interface{}, key string) (string, error) {
	v, ok := payload[key]
	if !ok {
		return "", fmt.Errorf("payload.%s required", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("payload.%s must be string", key)
	}
	return s, nil
}

func goodsPayload(payload map[string]interface{}, key string) (model.GoodsID, error) {
	v, ok := payload[key]
	if !ok {
		return 0, fmt.Errorf("payload.%s required", key)
	}
	return goodsFromAny(v)
}

func goodsFromAny(v interface{}) (model.GoodsID, error) {
	n, err := toInt(v)
	if err != nil {
		return 0, err
	}
	gid := model.GoodsID(n)
	if gid < model.GoodsGinseng || gid > model.GoodsJade {
		return 0, fmt.Errorf("invalid goods id %d", n)
	}
	return gid, nil
}

func goodsListPayload(payload map[string]interface{}, key string) ([]model.GoodsID, error) {
	v, ok := payload[key]
	if !ok {
		return nil, fmt.Errorf("payload.%s required", key)
	}
	raw, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("payload.%s must be array", key)
	}
	out := []model.GoodsID{}
	for _, item := range raw {
		gid, err := goodsFromAny(item)
		if err != nil {
			return nil, err
		}
		out = append(out, gid)
	}
	return out, nil
}

func toInt(v interface{}) (int, error) {
	switch t := v.(type) {
	case int:
		return t, nil
	case int64:
		return int(t), nil
	case float64:
		return int(t), nil
	case jsonNumber:
		i, err := strconv.Atoi(string(t))
		return i, err
	default:
		return 0, errors.New("value must be number")
	}
}

type jsonNumber string
