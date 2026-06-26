package app

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	"manila/internal/model"
	"manila/internal/rules"
	"manila/internal/store"
)

func TestTutorialChaptersAreMergedAndOldIDsMap(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	chapters := svc.TutorialChapters()
	if len(chapters) != 5 {
		t.Fatalf("expected 5 tutorial chapters, got %+v", chapters)
	}
	if chapters[0].ID != "overview" || chapters[1].ID != "intro" || chapters[2].ID != "placement_main" || chapters[3].ID != "voyage" || chapters[4].ID != "settlement_money" {
		t.Fatalf("unexpected merged chapters: %+v", chapters)
	}
	for oldID, want := range map[string]string{
		"overview":      "overview",
		"auction":       "intro",
		"harbor":        "intro",
		"placement":     "placement_main",
		"special":       "placement_main",
		"pirate_board":  "voyage",
		"pirate_loot":   "voyage",
		"navigator_big": "voyage",
		"settlement":    "settlement_money",
		"mortgage":      "settlement_money",
		"bankrupt":      "settlement_money",
	} {
		_, tutorial, err := svc.CreateTutorial(oldID)
		if err != nil {
			t.Fatalf("create old chapter %s: %v", oldID, err)
		}
		if tutorial.ChapterID != want {
			t.Fatalf("old chapter %s mapped to %s, want %s", oldID, tutorial.ChapterID, want)
		}
	}
}

func TestTutorialStepsHaveClearGuideTargetsAndNoHighlightWording(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	classroomPhrases := []string{"高亮", "本教学", "本步", "适合用来", "方便理解", "学习", "通常不会", "急着"}
	forbiddenTitlePhrases := []string{
		"拿下",
		"买海盗",
		"做海盗",
		"做保险商",
		"先上",
		"再上",
		"再买一个",
		"再买人参股份",
		"投入没有回报",
		"继续加价",
		"押至少",
		"劫掠后送往港口",
		"第 1 轮",
		"第 2 轮",
		"第 3 轮",
		"第 4 轮",
		"第 5 轮",
		"第一轮",
		"第二轮",
		"第三轮",
		"第四轮",
		"第五轮",
		"没有现金时",
		"没钱时",
	}
	allowedQuotedButtonLabels := map[string]bool{
		"出价":     true,
		"放弃":     true,
		"海盗留在船上": true,
		"确认出航":   true,
		"确认本轮结算": true,
		"移动":     true,
		"跳过购买股份": true,
	}
	quotedText := regexp.MustCompile(`“([^”]+)”`)
	scriptIntentPhrases := []string{
		"本章从",
		"按正常流程",
		"继续保留竞拍流程",
		"正常流程处理",
		"主要结算都已经看完",
		"继续观察",
		"看看",
		"现在进入正常放置",
		"成交后现金会归零",
		"后续付费",
		"接下来的付费",
		"会触发自动抵押",
		"会同时观察",
		"后面触发",
		"机会变多",
		"最后一轮",
		"接近最高市值",
		"代替你",
	}
	preResultSpoilers := map[string][]string{
		"round4-place-pirate":   {"停在 13", "刚好", "升到 30", "结束"},
		"round4-place-silk":     {"已经接近到港", "跑得很快", "升到 30", "结束"},
		"confirm-round4-profit": {"升到 30", "结束本章"},
		"round5-place-ginseng":  {"再到港一次", "升到最高", "升到 30", "结束"},
		"round5-place-silk":     {"跑得很快", "升到 30", "结束"},
		"round5-place-port":     {"稍后到港", "会排到", "升到 30", "结束"},
	}
	for _, chapter := range tutorialChapters() {
		g, steps, err := svc.buildTutorialGame(&chapter, "guide-check-"+chapter.ID)
		if err != nil {
			t.Fatalf("build tutorial %s: %v", chapter.ID, err)
		}
		if len(steps) == 0 {
			t.Fatalf("chapter %s has no tutorial steps", chapter.ID)
		}
		for _, step := range steps {
			if step.ID == "" || step.Title == "" || step.Body == "" {
				t.Fatalf("chapter %s has incomplete step metadata: %+v", chapter.ID, step)
			}
			for _, phrase := range forbiddenTitlePhrases {
				if strings.Contains(step.Title, phrase) {
					t.Fatalf("chapter %s step %s has informal or misleading title phrase %q: %q", chapter.ID, step.ID, phrase, step.Title)
				}
			}
			if step.ActionType == "" {
				t.Fatalf("chapter %s step %s has no action type", chapter.ID, step.ID)
			}
			if step.ActionType != model.ActionTutorialContinue {
				if strings.Count(step.Body, "**")%2 != 0 {
					t.Fatalf("chapter %s step %s has unmatched emphasis marker: %q", chapter.ID, step.ID, step.Body)
				}
				if !strings.Contains(step.Body, "**") {
					t.Fatalf("chapter %s step %s should mark the required action in the backend body: %q", chapter.ID, step.ID, step.Body)
				}
			}
			if step.Target == "" && len(step.Targets) == 0 {
				t.Fatalf("chapter %s step %s has no guide target", chapter.ID, step.ID)
			}
			if regexp.MustCompile(`^以 \d+ 成交$`).MatchString(step.Title) {
				t.Fatalf("chapter %s step %s uses scripted auction title %q", chapter.ID, step.ID, step.Title)
			}
			for _, phrase := range classroomPhrases {
				if strings.Contains(step.Title, phrase) || strings.Contains(step.Body, phrase) || strings.Contains(chapter.Description, phrase) {
					t.Fatalf("chapter %s step %s uses classroom wording %q: %q / %q / %q", chapter.ID, step.ID, phrase, chapter.Description, step.Title, step.Body)
				}
			}
			for _, phrase := range scriptIntentPhrases {
				if strings.Contains(step.Title, phrase) || strings.Contains(step.Body, phrase) {
					t.Fatalf("chapter %s step %s reveals script intent %q: %q / %q", chapter.ID, step.ID, phrase, step.Title, step.Body)
				}
			}
			for _, phrase := range preResultSpoilers[step.ID] {
				if strings.Contains(step.Title, phrase) || strings.Contains(step.Body, phrase) {
					t.Fatalf("chapter %s step %s reveals future outcome %q: %q / %q", chapter.ID, step.ID, phrase, step.Title, step.Body)
				}
			}
			if step.ID == "round4-place-port" {
				if strings.Contains(step.Title, "第一艘") || strings.Contains(step.Body, "第一艘") {
					t.Fatalf("round4-place-port should describe port A as at least one arrival, got %q / %q", step.Title, step.Body)
				}
				for _, phrase := range []string{"至少一艘", "4 比索", "6 比索"} {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("round4-place-port should explain the stable port A payoff %q, got %q", phrase, step.Body)
					}
				}
			}
			if step.ID == "round5-place-port" {
				if strings.Contains(step.Title, "第二艘") || strings.Contains(step.Body, "第二艘") || strings.Contains(step.Body, "占了港口") {
					t.Fatalf("round5-place-port should avoid misleading port wording, got %q / %q", step.Title, step.Body)
				}
				for _, phrase := range []string{"至少两艘船到港", "港口 B"} {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("round5-place-port should explain port B condition %q, got %q", phrase, step.Body)
					}
				}
			}
			if step.ID == "pirate-skip-board" {
				if strings.Contains(step.Body, "点击“放弃”") || strings.Contains(step.Body, "**请点击“放弃”**") {
					t.Fatalf("pirate-skip-board should name the actual action button, got %q", step.Body)
				}
				if !strings.Contains(step.Body, "海盗留在船上") {
					t.Fatalf("pirate-skip-board should name the actual action button, got %q", step.Body)
				}
			}
			if strings.Contains(step.Body, "确认结算") {
				t.Fatalf("tutorial should use the actual ConfirmRound button label 确认本轮结算, got %q", step.Body)
			}
			for _, match := range quotedText.FindAllStringSubmatch(step.Body, -1) {
				if len(match) < 2 || !allowedQuotedButtonLabels[match[1]] {
					t.Fatalf("chapter %s step %s quotes a non-button or unknown label %q in %q", chapter.ID, step.ID, match[1], step.Body)
				}
			}
			if step.ID == "insurance-loss-break" {
				for _, phrase := range []string{"船坞收益由保险商支付", "最低扣到 0", "银行补足"} {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("insurance-loss-break should explain insurance settlement %q, got %q", phrase, step.Body)
					}
				}
			}
			if step.ID == "stowaway-payout-break" {
				for _, phrase := range []string{"最低费用规则", "参与船上分账", "重新有了现金"} {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("stowaway-payout-break should explain stowaway payout %q, got %q", phrase, step.Body)
					}
				}
			}
			if step.ID == "voyage-pirate" {
				for _, phrase := range []string{"每艘出航货船", "独立掷骰", "1 到 6 格"} {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("voyage-pirate should explain dice movement rule %q, got %q", phrase, step.Body)
					}
				}
			}
			for stepID, phrases := range map[string][]string{
				"first-sail-break":        {"各自掷骰", "事件区", "点数"},
				"second-sail-break":       {"第 2 次航行", "停在 13", "空位", "船长先决定"},
				"pirate-board":            {"空位", "海盗船长", "先由你决定"},
				"second-round-sail-break": {"第 2 次航行", "停在 13", "空位", "船长先决定"},
			} {
				if step.ID != stepID {
					continue
				}
				for _, phrase := range phrases {
					if !strings.Contains(step.Body, phrase) {
						t.Fatalf("%s should explain sailing result %q, got %q", step.ID, phrase, step.Body)
					}
				}
			}
		}
		if g == nil {
			t.Fatalf("chapter %s did not build a game", chapter.ID)
		}
	}
	source, err := os.ReadFile("tutorial.go")
	if err != nil {
		t.Fatalf("read tutorial source: %v", err)
	}
	for _, phrase := range []string{
		"代替你完成了其他玩家",
		"系统用合法动作快速完成",
		"看竞拍怎样",
		"体验竞拍",
		"值得先盯住",
		"穿插看",
		"系统随后会把",
		"系统会让副手",
		"其他玩家放置和航行",
		"系统玩家完成了其他放置",
		"系统玩家补齐了第 2 次放置",
		"刚才系统完成了第 1 轮",
		"海盗通常要等船停在 13",
		"这里先由系统完成本轮剩余流程",
		"押第二艘到港",
		"占了港口 A",
		"系统完成了剩余放置和移动",
		"这一轮很倒霉",
		"没有船上收益，海盗也没有劫掠收益",
		"系统完成竞拍和出航准备",
		"系统完成本轮出航准备",
		"当前起点靠前",
		"竞拍结束后，最高出价者成为港务长",
		"竞拍结束后按成交价支付",
		"系统玩家之后会轮流回应",
		"系统玩家之后会轮流出价或放弃",
		"本轮可以先让肉豆蔻留在岸上",
		"这是一个条件已经满足后的简单选择",
		"点击“不买”",
		"这一轮你要亲手放完",
		"轮到你放第二个同伙",
		"把第三个同伙放到",
		"最后一个同伙",
		"这一轮先把重点",
		"本轮结算已经完成。请点击",
		"手动点",
	} {
		if strings.Contains(string(source), phrase) {
			t.Fatalf("tutorial source contains forbidden wording %q", phrase)
		}
	}
}

func TestIntroTutorialTwoRoundFlow(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g, tutorial, err := svc.CreateTutorial("auction")
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.ChapterID != "intro" || tutorial.StepID != "opening-bid" {
		t.Fatalf("unexpected tutorial view: %+v", tutorial)
	}
	if tutorial.Target != "#bidAmountInput" || len(tutorial.Targets) != 2 || tutorial.Targets[0] != "#bidAmountInput" || tutorial.Targets[1] != "#bidSubmitBtn" {
		t.Fatalf("expected opening bid to guide both bid input and submit button, got %+v", tutorial)
	}
	actions, _, err := svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !hasTutorialAction(actions, model.ActionBid) || !hasTutorialAction(actions, model.ActionPassBid) {
		t.Fatalf("expected tutorial actions to expose legal bid and pass choices, got %+v", actions)
	}
	if _, _, err := svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPassBid}); err == nil {
		t.Fatal("expected pass to be rejected before the first bid")
	}

	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "raise-to-eight" || g.Phase != model.PhaseAuction || g.Auction.CurrentBid != 5 || g.Auction.HighestBidder != 2 {
		t.Fatalf("expected P2 first raise and raise step, phase=%s bid=%d high=%d tutorial=%+v", g.Phase, g.Auction.CurrentBid, g.Auction.HighestBidder, tutorial)
	}

	if _, _, err := svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 5}}); err == nil {
		t.Fatal("expected equal bid to be rejected")
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 8}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "raise-to-eighteen" || g.Phase != model.PhaseAuction || g.Auction.CurrentBid != 15 || g.Auction.HighestBidder != 2 {
		t.Fatalf("expected P2 second raise, phase=%s bid=%d high=%d tutorial=%+v", g.Phase, g.Auction.CurrentBid, g.Auction.HighestBidder, tutorial)
	}

	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 18}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "pass-high-bid" || g.Phase != model.PhaseAuction || g.Auction.CurrentBid != 25 || g.Auction.HighestBidder != 2 {
		t.Fatalf("expected P2 high bid and pass step, phase=%s bid=%d high=%d tutorial=%+v", g.Phase, g.Auction.CurrentBid, g.Auction.HighestBidder, tutorial)
	}

	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPassBid})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-round-raise" || g.Phase != model.PhaseAuction || g.RoundNumber != 2 || g.CurrentPlayer != 1 || g.Auction.CurrentBid != 5 || g.Auction.HighestBidder != 4 {
		t.Fatalf("expected second-round P1 auction, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}

	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 8}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-round-win" || g.Phase != model.PhaseAuction || g.Auction.CurrentBid != 11 || g.Auction.HighestBidder != 3 {
		t.Fatalf("expected P3 second-round raise to 11, phase=%s bid=%d high=%d tutorial=%+v", g.Phase, g.Auction.CurrentBid, g.Auction.HighestBidder, tutorial)
	}

	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 14}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "buy-share" || g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != 1 {
		t.Fatalf("expected P1 harbor master buy-share step, phase=%s hm=%d tutorial=%+v", g.Phase, g.HarborMaster, tutorial)
	}
	if len(tutorial.Targets) != 2 || tutorial.Targets[0] != `[data-goods-id="1"]` || tutorial.Targets[1] != tutorialActionButtonTarget(model.ActionBuyShare, `{\"goodsId\":1}`) {
		t.Fatalf("buy share should guide both board goods and action button, got %+v", tutorial.Targets)
	}

	_, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBuyShare, Payload: map[string]interface{}{"goodsId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "select-goods" {
		t.Fatalf("expected select-goods, got %+v", tutorial)
	}
	if len(tutorial.Targets) != 4 || tutorial.Targets[0] != `[data-goods-id="1"]` || tutorial.Targets[1] != `[data-goods-id="2"]` || tutorial.Targets[2] != `[data-goods-id="3"]` {
		t.Fatalf("select-goods should expose all correct guide targets, got %+v", tutorial.Targets)
	}
	_, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "set-starts" {
		t.Fatalf("expected set-starts, got %+v", tutorial)
	}
	_, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 5, "2": 4, "3": 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if !tutorial.Completed {
		t.Fatalf("expected intro completion, got %+v", tutorial)
	}
}

func TestPlacementTutorialUsesTwoThreePlacementRounds(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g, tutorial, err := svc.CreateTutorial("placement_main")
	if err != nil {
		t.Fatal(err)
	}
	if g.Ships[model.GoodsGinseng].Position != 5 || g.Ships[model.GoodsNutmeg].Position != 4 || g.Ships[model.GoodsSilk].Position != 0 {
		t.Fatalf("placement tutorial should start with two fast ships and one slow ship, got ginseng=%d nutmeg=%d silk=%d",
			g.Ships[model.GoodsGinseng].Position, g.Ships[model.GoodsNutmeg].Position, g.Ships[model.GoodsSilk].Position)
	}
	actionTargets := map[string]string{
		"place-ship":            tutorialPlaceButtonTarget("ship", 1),
		"place-port":            tutorialPlaceButtonTarget("port", "B"),
		"place-dock":            tutorialPlaceButtonTarget("dock", "A"),
		"place-pirate":          tutorialPlaceButtonTarget("pirate", 1),
		"place-insurance":       tutorialPlaceButtonTarget("insurance", "insurance"),
		"place-small-navigator": tutorialPlaceButtonTarget("navigatorSmall", "small"),
	}
	for _, tc := range []struct {
		stepID       string
		action       model.Action
		nextStepID   string
		wantRound    int
		wantStep     int
		wantTurns    int
		wantPhase    model.Phase
		wantComplete bool
	}{
		{"place-ship", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}}, "place-port", 1, 2, 0, model.PhasePlacement, false},
		{"place-port", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}}, "place-dock", 1, 3, 0, model.PhasePlacement, false},
		{"place-dock", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}}, "placement-round-break", 2, 1, 0, model.PhasePlacement, false},
		{"place-pirate", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}}, "place-insurance", 2, 2, 0, model.PhasePlacement, false},
		{"place-insurance", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}}, "place-small-navigator", 2, 3, 0, model.PhasePlacement, false},
		{"place-small-navigator", model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorSmall", "targetId": "small"}}, "", 2, 3, 0, model.PhaseRoundReview, true},
	} {
		if tutorial.StepID != tc.stepID {
			t.Fatalf("expected step %s, got %+v", tc.stepID, tutorial)
		}
		if want := actionTargets[tc.stepID]; want != "" && !tutorialTargetsInclude(tutorial.Targets, want) {
			t.Fatalf("step %s should also guide action button %s, got %+v", tc.stepID, want, tutorial.Targets)
		}
		g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, tc.action)
		if err != nil {
			t.Fatalf("apply %s: %v", tc.stepID, err)
		}
		if tutorial.Completed != tc.wantComplete {
			t.Fatalf("completion after %s = %v", tc.stepID, tutorial.Completed)
		}
		if !tc.wantComplete && tutorial.StepID != tc.nextStepID {
			t.Fatalf("after %s expected next %s, got %+v", tc.stepID, tc.nextStepID, tutorial)
		}
		if tc.stepID == "place-dock" {
			if !tutorialEventsIncludeShip(g.Events, "ShipArrived", 1) || !tutorialEventsIncludeShip(g.Events, "ShipArrived", 2) || !tutorialEventsIncludeShip(g.Events, "ShipDocked", 3) {
				t.Fatalf("placement first round should produce two arrivals and one dock, events=%+v", g.Events)
			}
		}
		if g.RoundNumber != tc.wantRound || g.Round.PlacementStep != tc.wantStep || g.Round.PlacementTurnsTaken != tc.wantTurns || g.Phase != tc.wantPhase {
			t.Fatalf("after %s got round=%d placementStep=%d turns=%d phase=%s ships=%d/%d/%d dice=%+v", tc.stepID, g.RoundNumber, g.Round.PlacementStep, g.Round.PlacementTurnsTaken, g.Phase,
				g.Ships[model.GoodsGinseng].Position, g.Ships[model.GoodsNutmeg].Position, g.Ships[model.GoodsSilk].Position, g.Round.DiceResults)
		}
		if tc.nextStepID == "placement-round-break" {
			assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
			g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
			if err != nil {
				t.Fatalf("continue placement breakpoint: %v", err)
			}
			if tutorial.StepID != "place-pirate" {
				t.Fatalf("expected place-pirate after placement breakpoint, got %+v", tutorial)
			}
		}
	}
}

func tutorialEventsIncludeShip(events []model.Event, eventType string, shipID int) bool {
	for _, event := range events {
		if event.Type == eventType && fmt.Sprint(event.Data["shipId"]) == fmt.Sprint(shipID) {
			return true
		}
	}
	return false
}

func tutorialEventsIncludePiratePayout(events []model.Event, playerID int) bool {
	for _, event := range events {
		if event.Type == "PlayerReceivedPayout" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) &&
			fmt.Sprint(event.Data["source"]) == "pirates" {
			return true
		}
	}
	return false
}

func tutorialEventsIncludeInsurancePayment(events []model.Event, playerID int) bool {
	for _, event := range events {
		if event.Type == "InsurancePaidDockReward" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) {
			return true
		}
	}
	return false
}

func tutorialEventsIncludePayoutSource(events []model.Event, playerID int, source string) bool {
	for _, event := range events {
		if event.Type == "PlayerReceivedPayout" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) &&
			fmt.Sprint(event.Data["source"]) == source {
			return true
		}
	}
	return false
}

func tutorialEventsIncludeMarketAdvance(events []model.Event, goodsID model.GoodsID) bool {
	for _, event := range events {
		if event.Type == "GoodsMarketAdvanced" &&
			fmt.Sprint(event.Data["goodsId"]) == fmt.Sprint(goodsID) {
			return true
		}
	}
	return false
}

func tutorialEventsIncludeMarketAdvanceAfter(events []model.Event, goodsID model.GoodsID, afterSeq int) bool {
	for _, event := range events {
		if event.Seq > afterSeq &&
			event.Type == "GoodsMarketAdvanced" &&
			fmt.Sprint(event.Data["goodsId"]) == fmt.Sprint(goodsID) {
			return true
		}
	}
	return false
}

func tutorialLatestHarborMasterPrice(events []model.Event, playerID int) int {
	price := -1
	for _, event := range events {
		if event.Type == "HarborMasterSet" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) {
			if value, err := toTestInt(event.Data["price"]); err == nil {
				price = value
			}
		}
	}
	return price
}

func tutorialRoundStartedSeq(events []model.Event, roundNumber int) int {
	seq := -1
	for _, event := range events {
		if event.Type == "RoundStarted" &&
			fmt.Sprint(event.Data["roundNumber"]) == fmt.Sprint(roundNumber) {
			seq = event.Seq
		}
	}
	return seq
}

func tutorialInsurancePaidAfter(events []model.Event, playerID int, afterSeq int) int {
	total := 0
	for _, event := range events {
		if event.Seq <= afterSeq || event.Type != "InsurancePaidDockReward" ||
			fmt.Sprint(event.Data["playerId"]) != fmt.Sprint(playerID) {
			continue
		}
		if value, err := toTestInt(event.Data["amount"]); err == nil {
			total += value
		}
	}
	return total
}

func tutorialPayoutSourceAfter(events []model.Event, playerID int, source string, afterSeq int) bool {
	for _, event := range events {
		if event.Seq > afterSeq &&
			event.Type == "PlayerReceivedPayout" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) &&
			fmt.Sprint(event.Data["source"]) == source {
			return true
		}
	}
	return false
}

func tutorialPayoutAfter(events []model.Event, playerID int, source string, shipID model.GoodsID, afterSeq int) int {
	total := 0
	for _, event := range events {
		if event.Seq > afterSeq &&
			event.Type == "PlayerReceivedPayout" &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) &&
			fmt.Sprint(event.Data["source"]) == source &&
			fmt.Sprint(event.Data["shipId"]) == fmt.Sprint(shipID) {
			if value, err := toTestInt(event.Data["amount"]); err == nil {
				total += value
			}
		}
	}
	return total
}

func tutorialFinalRank(g *model.Game, playerID int) int {
	for _, score := range g.FinalScores {
		if score.PlayerID == playerID {
			return score.Rank
		}
	}
	return 0
}

func tutorialFinalWealth(g *model.Game, playerID int) int {
	for _, score := range g.FinalScores {
		if score.PlayerID == playerID {
			return score.Wealth
		}
	}
	return 0
}

func tutorialEventTypeForPlayerAfter(events []model.Event, eventType string, playerID int, afterSeq int) bool {
	for _, event := range events {
		if event.Seq > afterSeq &&
			event.Type == eventType &&
			fmt.Sprint(event.Data["playerId"]) == fmt.Sprint(playerID) {
			return true
		}
	}
	return false
}

func tutorialPlacementAfter(events []model.Event, playerID int, positionType string, shipID model.GoodsID, cost int, afterSeq int) bool {
	for _, event := range events {
		if event.Seq <= afterSeq || event.Type != "AccomplicePlaced" ||
			fmt.Sprint(event.Data["playerId"]) != fmt.Sprint(playerID) ||
			fmt.Sprint(event.Data["positionType"]) != positionType ||
			fmt.Sprint(event.Data["shipId"]) != fmt.Sprint(shipID) {
			continue
		}
		if got, err := toTestInt(event.Data["cost"]); err == nil && got == cost {
			return true
		}
	}
	return false
}

func toTestInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("not an int: %v", value)
	}
}

func assertEventOrder(t *testing.T, events []model.Event, beforeType, afterType string) {
	t.Helper()
	before := -1
	after := -1
	for i, event := range events {
		if event.Type == beforeType {
			before = i
		}
		if event.Type == afterType {
			after = i
		}
	}
	if before < 0 || after < 0 || before >= after {
		t.Fatalf("expected latest %s before latest %s, got before=%d after=%d events=%+v", beforeType, afterType, before, after, events)
	}
}

func assertOnlyTutorialContinue(t *testing.T, svc *Service, sessionID string) {
	t.Helper()
	actions, _, err := svc.TutorialActions(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionTutorialContinue {
		t.Fatalf("expected only tutorial continue action, got %+v", actions)
	}
}

func TestVoyageTutorialNaturallyReachesPiratesAndNavigator(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g, tutorial, err := svc.CreateTutorial("voyage")
	if err != nil {
		t.Fatal(err)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("pirate", 1)) {
		t.Fatalf("voyage pirate step should guide both board and action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "first-sail-break" || g.Phase != model.PhasePlacement || g.Round.PlacementStep != 2 {
		t.Fatalf("expected first sailing breakpoint before second placement, phase=%s step=%d tutorial=%+v", g.Phase, g.Round.PlacementStep, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "voyage-ship" || !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("ship", 2)) {
		t.Fatalf("expected voyage ship step, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-sail-break" || g.Phase != model.PhasePirateBoarding {
		t.Fatalf("expected second sailing breakpoint before pirate boarding, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "pirate-board" || g.Phase != model.PhasePirateBoarding {
		t.Fatalf("expected natural pirate boarding after breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPirateBoardButtonTarget(3)) {
		t.Fatalf("pirate board should guide ship and board action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateBoard, Payload: map[string]interface{}{"shipId": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "place-small-navigator-voyage" || g.Phase != model.PhasePlacement || g.Round.PlacementStep != 3 {
		t.Fatalf("expected third placement after boarding, phase=%s step=%d tutorial=%+v", g.Phase, g.Round.PlacementStep, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorSmall", "targetId": "small"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "mate-leads-break" || g.Phase != model.PhaseNavigatorAction {
		t.Fatalf("expected mate explanation before navigator action, got phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if len(g.Board.Pirates) != 1 || g.Board.Pirates[0].PlayerID != 2 || g.Board.Pirates[0].Role != "mate" {
		t.Fatalf("expected system mate to remain on pirate ship after captain boarded, pirates=%+v", g.Board.Pirates)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "small-navigator-move" || g.Phase != model.PhaseNavigatorAction {
		t.Fatalf("expected small navigator move after breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, `.nav-move-input[data-ship-id="2"]`) {
		t.Fatalf("small navigator should guide the nutmeg move input, got %+v", tutorial.Targets)
	}
	if g.Ships[model.GoodsNutmeg].Position != 11 {
		t.Fatalf("small navigator should start with nutmeg at 11, got %d", g.Ships[model.GoodsNutmeg].Position)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionNavigatorMove, Payload: map[string]interface{}{"moves": []interface{}{map[string]interface{}{"shipId": 2, "delta": 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "mate-decision-break" || g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 2 {
		t.Fatalf("expected mate-led pirate looting breakpoint, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	if g.Ships[model.GoodsGinseng].Position != 13 {
		t.Fatalf("small navigator move should preserve ginseng pirate trigger at 13, got %d", g.Ships[model.GoodsGinseng].Position)
	}
	if len(g.Round.PendingLootShips) != 1 || g.Round.PendingLootShips[0] != model.GoodsGinseng {
		t.Fatalf("small navigator round should trigger pirate looting from dice on ginseng, got %+v", g.Round.PendingLootShips)
	}
	assertEventOrder(t, g.Events, "NavigatorMoved", "DiceRolled")
	actions, _, err := svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionTutorialContinue {
		t.Fatalf("expected tutorial continue action at mate breakpoint, got %+v", actions)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-round-pirate" || g.RoundNumber != 2 || g.Phase != model.PhasePlacement {
		t.Fatalf("expected second pirate round setup, round=%d phase=%s tutorial=%+v", g.RoundNumber, g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "place-big-navigator" || !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("navigatorBig", "big")) {
		t.Fatalf("expected big navigator after system mate placement, got %+v", tutorial)
	}
	if len(g.Board.Pirates) != 2 || g.Board.Pirates[0].PlayerID != 1 || g.Board.Pirates[1].PlayerID != 2 {
		t.Fatalf("expected captain and mate on pirate ship, got %+v", g.Board.Pirates)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorBig", "targetId": "big"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-round-sail-break" || g.Phase != model.PhasePirateBoarding || g.CurrentPlayer != 1 {
		t.Fatalf("expected second-round sailing breakpoint before captain boarding choice, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "pirate-skip-board" || g.Phase != model.PhasePirateBoarding || g.CurrentPlayer != 1 {
		t.Fatalf("expected captain boarding choice after breakpoint, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateSkipBoard})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "second-round-ship" || g.Phase != model.PhasePlacement || g.Round.PlacementStep != 3 {
		t.Fatalf("expected third placement after captain and mate skip boarding, phase=%s step=%d tutorial=%+v", g.Phase, g.Round.PlacementStep, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "big-navigator-move" || g.Phase != model.PhaseNavigatorAction {
		t.Fatalf("expected big navigator move after third placement, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if g.Ships[model.GoodsNutmeg].Position != 13 {
		t.Fatalf("big navigator should start with nutmeg already at 13 before moving it away, got %d", g.Ships[model.GoodsNutmeg].Position)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionNavigatorMove, Payload: map[string]interface{}{"moves": []interface{}{map[string]interface{}{"shipId": 1, "delta": 1}, map[string]interface{}{"shipId": 2, "delta": -1}}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "pirate-loot-captain" || g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 1 {
		t.Fatalf("expected captain-led pirate looting, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	if g.Ships[model.GoodsNutmeg].Status != model.ShipArrived || g.Ships[model.GoodsSilk].Position != 13 {
		t.Fatalf("big navigator should move nutmeg off 13 and dice should leave silk at 13, nutmeg=%+v silk=%+v", g.Ships[model.GoodsNutmeg], g.Ships[model.GoodsSilk])
	}
	if len(g.Round.PendingLootShips) != 1 || g.Round.PendingLootShips[0] != model.GoodsSilk {
		t.Fatalf("big navigator round should trigger pirate looting from dice on silk, got %+v", g.Round.PendingLootShips)
	}
	assertEventOrder(t, g.Events, "NavigatorMoved", "DiceRolled")
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPirateDestinationButtonTarget(3, "port")) {
		t.Fatalf("captain looting should guide destination button too, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 3, "destination": "port"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "pirate-payout-break" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected pirate payout breakpoint after looting, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if !tutorialEventsIncludePiratePayout(g.Events, 1) || !tutorialEventsIncludePiratePayout(g.Events, 2) {
		t.Fatalf("expected captain and mate to split pirate payout, events=%+v", g.Events)
	}
}

func TestSettlementTutorialMortgageAndBankruptStowaway(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g, tutorial, err := svc.CreateTutorial("settlement_money")
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-auction-bid" || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 {
		t.Fatalf("expected first settlement auction, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 12}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-auction-win" || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 || g.Auction.CurrentBid != 16 {
		t.Fatalf("expected first settlement auction win step, phase=%s current=%d bid=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Auction.CurrentBid, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 20}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-skip-share" || g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != 1 {
		t.Fatalf("expected manual skip-share step after first auction, phase=%s hm=%d tutorial=%+v", g.Phase, g.HarborMaster, tutorial)
	}
	if price := tutorialLatestHarborMasterPrice(g.Events, 1); price != 20 {
		t.Fatalf("expected first settlement auction to close at 20, price=%d events=%+v", price, g.Events)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSkipBuyShare})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-select-goods" {
		t.Fatalf("expected first settlement select goods, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-set-starts" {
		t.Fatalf("expected first settlement set starts, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 5, "2": 2, "3": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-place-ship" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Round.PlacementStep != 1 {
		t.Fatalf("expected first settlement placement, phase=%s current=%d step=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Round.PlacementStep, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("ship", 1)) {
		t.Fatalf("settlement ship placement should guide both board and action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-place-pirate" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Round.PlacementStep != 2 {
		t.Fatalf("expected second settlement placement, phase=%s current=%d step=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Round.PlacementStep, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("pirate", 1)) {
		t.Fatalf("settlement pirate placement should guide both board and action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-place-insurance" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Round.PlacementStep != 3 {
		t.Fatalf("expected third settlement placement, phase=%s current=%d step=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Round.PlacementStep, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("insurance", "insurance")) {
		t.Fatalf("settlement insurance placement should guide both board and action button, got %+v", tutorial.Targets)
	}
	if !tutorialTargetsInclude(tutorial.Targets, `.insurance-overlay .board-slot[data-slot-id="insurance"]`) {
		t.Fatalf("settlement insurance placement should guide the board insurance slot, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-looting-break" || g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 1 {
		t.Fatalf("expected pirate looting breakpoint, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	if len(g.Round.PendingLootShips) != 2 ||
		g.Round.PendingLootShips[0] != model.GoodsNutmeg ||
		g.Round.PendingLootShips[1] != model.GoodsSilk {
		t.Fatalf("expected nutmeg and silk pending pirate loot, got %+v", g.Round.PendingLootShips)
	}
	if g.Board.Insurance == nil || g.Board.Insurance.PlayerID != 1 {
		t.Fatalf("settlement tutorial should include P1 insurance merchant, got %+v", g.Board.Insurance)
	}
	if g.Board.Docks["A"].Occupant == nil || g.Board.Docks["A"].Occupant.PlayerID != 4 {
		t.Fatalf("settlement tutorial should include P4 on dock A, got %+v", g.Board.Docks["A"])
	}
	if len(g.Board.Pirates) != 2 || g.Board.Pirates[0].PlayerID != 1 || g.Board.Pirates[1].PlayerID != 2 {
		t.Fatalf("settlement tutorial should include P1 captain and P2 mate, got %+v", g.Board.Pirates)
	}
	if len(g.Ships[model.GoodsGinseng].Occupants) != 2 ||
		g.Ships[model.GoodsGinseng].Occupants[0].PlayerID != 1 ||
		g.Ships[model.GoodsGinseng].Occupants[1].PlayerID != 2 {
		t.Fatalf("settlement tutorial should put P1 and P2 on ginseng ship, got %+v", g.Ships[model.GoodsGinseng].Occupants)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "loot-to-port" || g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 1 {
		t.Fatalf("expected pirate looting choice, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPirateDestinationButtonTarget(2, "port")) {
		t.Fatalf("loot-to-port should guide the port destination action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 2, "destination": "port"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "loot-to-dock" || g.Phase != model.PhasePirateLooting || g.CurrentPlayer != 1 {
		t.Fatalf("expected second pirate looting choice, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPirateDestinationButtonTarget(3, "dock")) {
		t.Fatalf("loot-to-dock should guide the dock destination action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 3, "destination": "dock"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-ship-break" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected settlement ship breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	if !tutorialEventsIncludeShip(g.Events, "ShipArrived", int(model.GoodsGinseng)) ||
		!tutorialEventsIncludeShip(g.Events, "ShipArrived", int(model.GoodsNutmeg)) ||
		!tutorialEventsIncludeShip(g.Events, "ShipDocked", int(model.GoodsSilk)) {
		t.Fatalf("settlement tutorial should arrive ginseng/nutmeg and dock silk, events=%+v", g.Events)
	}
	if !tutorialEventsIncludeInsurancePayment(g.Events, 1) || !tutorialEventsIncludePayoutSource(g.Events, 4, "dock") {
		t.Fatalf("settlement tutorial should show P1 insurance paying dock reward to P4, events=%+v", g.Events)
	}
	if tutorialEventTypeForPlayerAfter(g.Events, "ShareBought", 1, 0) {
		t.Fatalf("settlement tutorial first round should not buy a share, events=%+v", g.Events)
	}
	if !tutorialEventsIncludePayoutSource(g.Events, 1, "ship") || !tutorialEventsIncludePayoutSource(g.Events, 2, "ship") {
		t.Fatalf("settlement tutorial should split ginseng ship payout between P1 and P2, events=%+v", g.Events)
	}
	if !tutorialEventsIncludePiratePayout(g.Events, 1) || !tutorialEventsIncludePiratePayout(g.Events, 2) {
		t.Fatalf("settlement tutorial should split pirate payout between captain and mate, events=%+v", g.Events)
	}
	if !tutorialEventsIncludeMarketAdvance(g.Events, model.GoodsGinseng) ||
		!tutorialEventsIncludeMarketAdvance(g.Events, model.GoodsNutmeg) ||
		tutorialEventsIncludeMarketAdvance(g.Events, model.GoodsSilk) {
		t.Fatalf("settlement tutorial should advance ginseng and looted-to-port nutmeg only, events=%+v", g.Events)
	}
	for _, want := range []string{"settlement-pirate-break", "settlement-insurance-break", "settlement-market-break", "confirm-settlement"} {
		g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
		if err != nil {
			t.Fatal(err)
		}
		if tutorial.StepID != want {
			t.Fatalf("expected %s, got %+v", want, tutorial)
		}
	}
	if tutorial.Target != tutorialActionButtonTarget(model.ActionConfirmRound, `{}`) {
		t.Fatalf("settlement confirm should guide the exact action button, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionConfirmRound})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-all-in-open" || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 || g.Players[1].Cash <= 0 {
		t.Fatalf("expected all-in opening auction step, phase=%s current=%d cash=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Players[1].Cash, tutorial)
	}
	if g.Players[1].MortgagedShareCount != 0 {
		t.Fatalf("all-in auction should not mortgage shares before bidding, got %d", g.Players[1].MortgagedShareCount)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 24}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-all-in-auction" || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 || g.Auction.CurrentBid != 36 {
		t.Fatalf("expected all-in auction close step, phase=%s current=%d bid=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Auction.CurrentBid, tutorial)
	}
	if g.Players[1].Cash != 44 {
		t.Fatalf("expected P1 to have 44 cash before final second-round bid, got %d", g.Players[1].Cash)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 44}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-all-in-skip-share" || g.Phase != model.PhaseHarborMasterBuyShare || g.Players[1].Cash != 0 {
		t.Fatalf("expected all-in skip-share step, phase=%s cash=%d tutorial=%+v", g.Phase, g.Players[1].Cash, tutorial)
	}
	if price := tutorialLatestHarborMasterPrice(g.Events, 1); price != 44 {
		t.Fatalf("expected P1 to win second auction at 44, price=%d events=%+v", price, g.Events)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSkipBuyShare})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-all-in-select-goods" {
		t.Fatalf("expected all-in select goods step, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "settlement-all-in-set-starts" {
		t.Fatalf("expected all-in set starts step, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 5, "2": 2, "3": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "auto-mortgage-pirate" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Players[1].Cash != 0 {
		t.Fatalf("expected mortgage pirate placement setup, phase=%s cash=%d tutorial=%+v", g.Phase, g.Players[1].Cash, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("pirate", 1)) {
		t.Fatalf("auto mortgage should guide pirate action button, got %+v", tutorial.Targets)
	}
	round2Seq := tutorialRoundStartedSeq(g.Events, 2)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "insurance-loss-ship" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Round.PlacementStep != 2 {
		t.Fatalf("expected insurance loss ship step, phase=%s current=%d step=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Round.PlacementStep, tutorial)
	}
	if g.Players[1].MortgagedShareCount != 1 {
		t.Fatalf("pirate placement should mortgage one share, got %d", g.Players[1].MortgagedShareCount)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "insurance-loss-insurance" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || g.Round.PlacementStep != 3 {
		t.Fatalf("expected insurance loss insurance step, phase=%s current=%d step=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Round.PlacementStep, tutorial)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialPlaceButtonTarget("insurance", "insurance")) ||
		!tutorialTargetsInclude(tutorial.Targets, `.insurance-overlay .board-slot[data-slot-id="insurance"]`) {
		t.Fatalf("insurance loss step should guide board and action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "insurance-loss-break" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected insurance loss breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if g.Ships[model.GoodsGinseng].Status != model.ShipDocked ||
		g.Ships[model.GoodsNutmeg].Status != model.ShipDocked ||
		g.Ships[model.GoodsSilk].Status != model.ShipDocked {
		t.Fatalf("second round should dock all ships, ginseng=%+v nutmeg=%+v silk=%+v", g.Ships[model.GoodsGinseng], g.Ships[model.GoodsNutmeg], g.Ships[model.GoodsSilk])
	}
	if paid := tutorialInsurancePaidAfter(g.Events, 1, round2Seq); paid >= 29 {
		t.Fatalf("expected P1 to run out of cash before paying full 29 insurance, paid=%d events=%+v", paid, g.Events)
	}
	if !tutorialEventTypeForPlayerAfter(g.Events, "BankCoveredInsuranceShortfall", 1, round2Seq) {
		t.Fatalf("expected bank to cover insurance shortfall once P1 has no cash or shares, events=%+v", g.Events)
	}
	if tutorialPayoutSourceAfter(g.Events, 1, "ship", round2Seq) || tutorialPayoutSourceAfter(g.Events, 1, "pirates", round2Seq) {
		t.Fatalf("P1 should not receive ship or pirate payout in second round, events=%+v", g.Events)
	}
	if g.Players[1].MortgagedShareCount != 2 || g.Players[1].Cash != 0 {
		t.Fatalf("insurance loss should mortgage both initial shares and reduce cash to 0, cash=%d mortgaged=%d", g.Players[1].Cash, g.Players[1].MortgagedShareCount)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "confirm-loss-settlement" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected confirm loss settlement, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionConfirmRound})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "bankruptcy-auction-pass" || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 || g.Players[1].Cash != 0 {
		t.Fatalf("expected bankrupt auction pass, phase=%s current=%d cash=%d tutorial=%+v", g.Phase, g.CurrentPlayer, g.Players[1].Cash, tutorial)
	}
	actions, _, err := svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionPassBid {
		t.Fatalf("expected only pass bid in bankrupt auction, got %+v", actions)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPassBid})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "bankruptcy-break" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 || !g.Players[1].BankruptThisRound {
		t.Fatalf("expected bankruptcy breakpoint, phase=%s current=%d bankrupt=%v tutorial=%+v", g.Phase, g.CurrentPlayer, g.Players[1].BankruptThisRound, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "stowaway" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 {
		t.Fatalf("expected bankrupt stowaway setup, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	round3Seq := tutorialRoundStartedSeq(g.Events, 3)
	actions, _, err = svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionPlaceAccomplice ||
		actions[0].Payload["isStowaway"] != true ||
		fmt.Sprint(actions[0].Payload["targetId"]) != "2" {
		t.Fatalf("expected only stowaway action, got %+v", actions)
	}
	if !tutorialTargetsInclude(tutorial.Targets, tutorialStowawayButtonTarget(2)) {
		t.Fatalf("stowaway should guide both board and action button, got %+v", tutorial.Targets)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "stowaway-tied-lowest" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 {
		t.Fatalf("expected second manual stowaway step, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	actions, _, err = svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionPlaceAccomplice ||
		actions[0].Payload["isStowaway"] != true ||
		fmt.Sprint(actions[0].Payload["targetId"]) != "2" {
		t.Fatalf("expected tied lowest stowaway to stay on nutmeg, got %+v", actions)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "stowaway-silk" || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 {
		t.Fatalf("expected third manual stowaway step, phase=%s current=%d tutorial=%+v", g.Phase, g.CurrentPlayer, tutorial)
	}
	actions, _, err = svc.TutorialActions(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Type != model.ActionPlaceAccomplice ||
		actions[0].Payload["isStowaway"] != true ||
		fmt.Sprint(actions[0].Payload["targetId"]) != "3" {
		t.Fatalf("expected final stowaway to move to silk, got %+v", actions)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "stowaway-payout-break" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected stowaway payout breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if g.Ships[model.GoodsGinseng].Status != model.ShipArrived ||
		g.Ships[model.GoodsNutmeg].Status != model.ShipArrived ||
		g.Ships[model.GoodsSilk].Status != model.ShipArrived {
		t.Fatalf("third round should arrive all ships, ginseng=%+v nutmeg=%+v silk=%+v", g.Ships[model.GoodsGinseng], g.Ships[model.GoodsNutmeg], g.Ships[model.GoodsSilk])
	}
	if !tutorialPlacementAfter(g.Events, 2, "ship", model.GoodsGinseng, 3, round3Seq) {
		t.Fatalf("expected system player to take the 3-cost ginseng slot, events=%+v", g.Events)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "ship", model.GoodsNutmeg, round3Seq); got != 24 {
		t.Fatalf("expected P1 to receive solo nutmeg ship payout 24, got %d events=%+v", got, g.Events)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "ship", model.GoodsSilk, round3Seq); got != 30 {
		t.Fatalf("expected P1 to receive solo silk ship payout 30, got %d events=%+v", got, g.Events)
	}

	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "confirm-stowaway-settlement" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected confirm stowaway settlement, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionConfirmRound})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-auction-bid" || g.RoundNumber != 4 || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 {
		t.Fatalf("expected round 4 auction bid, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}
	round4Seq := tutorialRoundStartedSeq(g.Events, 4)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 14}})
	if err != nil {
		t.Fatal(err)
	}
	if price := tutorialLatestHarborMasterPrice(g.Events, 1); price != 14 {
		t.Fatalf("expected round 4 auction to close at 14, price=%d events=%+v", price, g.Events)
	}
	if tutorial.StepID != "round4-buy-share" || g.RoundNumber != 4 || g.Phase != model.PhaseHarborMasterBuyShare || g.CurrentPlayer != 1 {
		t.Fatalf("expected round 4 buy-share step, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBuyShare, Payload: map[string]interface{}{"goodsId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-select-goods" || g.Phase != model.PhaseHarborMasterSelectGoods {
		t.Fatalf("expected round 4 select goods step, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-set-starts" || g.Phase != model.PhaseHarborMasterSetShips {
		t.Fatalf("expected round 4 set-starts step, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 4, "2": 0, "3": 5}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-place-silk" || g.RoundNumber != 4 || g.Phase != model.PhasePlacement || g.CurrentPlayer != 1 {
		t.Fatalf("expected round 4 silk placement, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-place-pirate" || g.Round.PlacementStep != 2 {
		t.Fatalf("expected round 4 pirate placement, step=%d tutorial=%+v", g.Round.PlacementStep, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-place-port" || g.Ships[model.GoodsSilk].Status != model.ShipArrived {
		t.Fatalf("expected round 4 port placement after silk arrival, silk=%+v tutorial=%+v", g.Ships[model.GoodsSilk], tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-loot-ginseng" || g.Phase != model.PhasePirateLooting {
		t.Fatalf("expected round 4 ginseng looting, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 1, "destination": "port"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round4-profit-break" || g.Phase != model.PhaseRoundReview {
		t.Fatalf("expected round 4 profit breakpoint, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "pirates", model.GoodsGinseng, round4Seq); got != 18 {
		t.Fatalf("expected P1 round 4 ginseng pirate payout 18, got %d events=%+v", got, g.Events)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "ship", model.GoodsSilk, round4Seq); got != 30 {
		t.Fatalf("expected P1 round 4 silk ship payout 30, got %d events=%+v", got, g.Events)
	}
	if !tutorialPayoutSourceAfter(g.Events, 1, "port", round4Seq) || g.Goods[model.GoodsGinseng].MarketValue() != 20 {
		t.Fatalf("expected round 4 port payout and ginseng value 20, value=%d events=%+v", g.Goods[model.GoodsGinseng].MarketValue(), g.Events)
	}

	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "confirm-round4-profit" {
		t.Fatalf("expected confirm round 4 profit, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionConfirmRound})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-auction-bid" || g.RoundNumber != 5 || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 {
		t.Fatalf("expected round 5 auction bid, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 8}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-auction-win" || g.RoundNumber != 5 || g.Phase != model.PhaseAuction || g.CurrentPlayer != 1 {
		t.Fatalf("expected round 5 second auction bid, round=%d phase=%s current=%d tutorial=%+v", g.RoundNumber, g.Phase, g.CurrentPlayer, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 16}})
	if err != nil {
		t.Fatal(err)
	}
	if price := tutorialLatestHarborMasterPrice(g.Events, 1); price != 16 {
		t.Fatalf("expected round 5 auction to close at 16, price=%d events=%+v", price, g.Events)
	}
	if tutorial.StepID != "round5-buy-share" || g.RoundNumber != 5 || g.Phase != model.PhaseHarborMasterBuyShare {
		t.Fatalf("expected round 5 buy-share step, round=%d phase=%s tutorial=%+v", g.RoundNumber, g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBuyShare, Payload: map[string]interface{}{"goodsId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-select-goods" || g.Phase != model.PhaseHarborMasterSelectGoods {
		t.Fatalf("expected round 5 select goods step, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 3, 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-set-starts" || g.Phase != model.PhaseHarborMasterSetShips {
		t.Fatalf("expected round 5 set-starts step, phase=%s tutorial=%+v", g.Phase, tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 5, "3": 4, "4": 0}}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-place-ginseng" || g.RoundNumber != 5 || g.Phase != model.PhasePlacement {
		t.Fatalf("expected round 5 ginseng placement, round=%d phase=%s tutorial=%+v", g.RoundNumber, g.Phase, tutorial)
	}
	round5Seq := tutorialRoundStartedSeq(g.Events, 5)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-place-silk" {
		t.Fatalf("expected round 5 silk placement, got %+v", tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-place-port" || g.Ships[model.GoodsSilk].Status != model.ShipArrived {
		t.Fatalf("expected round 5 port placement after silk arrival, silk=%+v tutorial=%+v", g.Ships[model.GoodsSilk], tutorial)
	}
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "round5-settlement-break" || g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		t.Fatalf("expected round 5 settlement breakpoint, status=%s phase=%s tutorial=%+v", g.Status, g.Phase, tutorial)
	}
	if g.Goods[model.GoodsGinseng].MarketValue() != 30 {
		t.Fatalf("expected ginseng 30 at game end, got ginseng=%d", g.Goods[model.GoodsGinseng].MarketValue())
	}
	if tutorialEventsIncludeMarketAdvanceAfter(g.Events, model.GoodsNutmeg, round5Seq) {
		t.Fatalf("round 5 should not advance nutmeg market, events=%+v", g.Events)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "ship", model.GoodsGinseng, round5Seq); got != 18 {
		t.Fatalf("expected P1 round 5 ginseng ship payout 18, got %d events=%+v", got, g.Events)
	}
	if got := tutorialPayoutAfter(g.Events, 1, "ship", model.GoodsSilk, round5Seq); got != 30 {
		t.Fatalf("expected P1 round 5 silk ship payout 30, got %d events=%+v", got, g.Events)
	}
	if !tutorialPayoutSourceAfter(g.Events, 1, "port", round5Seq) {
		t.Fatalf("expected P1 round 5 port payout, events=%+v", g.Events)
	}
	if rank := tutorialFinalRank(g, 1); rank != 1 {
		t.Fatalf("expected P1 to finish first, rank=%d scores=%+v", rank, g.FinalScores)
	}
	if wealth := tutorialFinalWealth(g, 1); wealth != 155 {
		t.Fatalf("expected P1 final wealth 155, wealth=%d scores=%+v", wealth, g.FinalScores)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "game-end-trigger-break" || g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		t.Fatalf("expected game end trigger breakpoint, status=%s phase=%s tutorial=%+v", g.Status, g.Phase, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "final-stock-scoring-break" || g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		t.Fatalf("expected final stock scoring breakpoint, status=%s phase=%s tutorial=%+v", g.Status, g.Phase, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "tutorial-victory-break" || g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		t.Fatalf("expected tutorial victory breakpoint, status=%s phase=%s tutorial=%+v", g.Status, g.Phase, tutorial)
	}
	assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
	_, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
	if err != nil {
		t.Fatal(err)
	}
	if !tutorial.Completed {
		t.Fatalf("expected settlement tutorial to complete after final game end, got %+v", tutorial)
	}
}

func TestOverviewTutorialAutoplaysCompleteGame(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	g, tutorial, err := svc.CreateTutorial("overview")
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.ChapterID != "overview" || tutorial.StepID != "overview-goal" {
		t.Fatalf("expected overview tutorial opening, got %+v", tutorial)
	}
	if g.Status != model.StatusNotStarted {
		t.Fatalf("overview should begin before the game starts, got %s", g.Status)
	}
	wantSteps := []string{
		"overview-goal",
		"overview-auction",
		"overview-preparation",
		"overview-placement",
		"overview-first-sailing",
		"overview-events",
		"overview-settlement",
		"overview-market",
		"overview-midgame",
		"overview-game-end",
		"overview-final-score",
	}
	seen := map[string]bool{}
	for !tutorial.Completed {
		stepID := tutorial.StepID
		seen[stepID] = true
		assertOnlyTutorialContinue(t, svc, tutorial.SessionID)
		beforeEventSeq := g.EventSeq
		g, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionTutorialContinue})
		if err != nil {
			t.Fatalf("continue overview step %s: %v", stepID, err)
		}
		if g.EventSeq <= beforeEventSeq {
			t.Fatalf("overview step %s did not advance event stream", stepID)
		}
	}
	for _, stepID := range wantSteps {
		if !seen[stepID] {
			t.Fatalf("overview tutorial did not visit step %s; visited %+v", stepID, seen)
		}
	}
	if g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		t.Fatalf("overview should finish after a complete game, status=%s phase=%s", g.Status, g.Phase)
	}
	if len(g.FinalScores) == 0 || g.FinalScores[0].PlayerID != tutorialPlayerID || g.FinalScores[0].Rank != 1 {
		t.Fatalf("overview should end with P1 in first place, got %+v", g.FinalScores)
	}
	if tutorial.CompletionTitle != "完成概览" || tutorial.CompletionBody == "" || tutorial.StepID != "" {
		t.Fatalf("expected overview completion panel, got %+v", tutorial)
	}
}

func TestTutorialRestartKeepsSessionAndResetsStep(t *testing.T) {
	svc := NewService(store.NewMemoryStore(), rules.NewEngine())
	_, tutorial, err := svc.CreateTutorial("harbor")
	if err != nil {
		t.Fatal(err)
	}
	_, tutorial, err = svc.ApplyTutorialAction(tutorial.SessionID, model.Action{Type: model.ActionBid, Payload: map[string]interface{}{"amount": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if tutorial.StepID != "raise-to-eight" {
		t.Fatalf("expected second step, got %+v", tutorial)
	}
	_, restarted, err := svc.RestartTutorial(tutorial.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.SessionID != tutorial.SessionID || restarted.ChapterID != "intro" || restarted.StepID != "opening-bid" || restarted.Completed {
		t.Fatalf("restart did not reset view: %+v", restarted)
	}
}

func hasTutorialAction(actions []model.LegalAction, actionType model.ActionType) bool {
	for _, action := range actions {
		if action.Type == actionType {
			return true
		}
	}
	return false
}

func tutorialTargetsInclude(targets []string, want string) bool {
	for _, target := range targets {
		if target == want {
			return true
		}
	}
	return false
}
