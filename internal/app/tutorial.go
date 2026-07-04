package app

import (
	"fmt"
	"sort"
	"strings"

	"manila/internal/model"
	"manila/internal/rules"
)

const tutorialPlayerID = 1

type tutorialSession struct {
	ID        string
	GameID    string
	ChapterID string
	StepIndex int
	Completed bool
}

type tutorialChapter struct {
	ID          string
	Title       string
	Description string
	Seed        int64
	Build       func(*Service, *model.Game) ([]tutorialStep, error)
}

type tutorialStep struct {
	ID         string
	Title      string
	Body       string
	Target     string
	Targets    []string
	ActionType model.ActionType
	Validate   func(model.Action) bool
	After      func(*Service, *model.Game) error
}

func (s *Service) TutorialChapters() []model.TutorialSummary {
	return tutorialChapterSummaries()
}

func (s *Service) CreateTutorial(chapterID string) (*model.Game, model.TutorialView, error) {
	chapter := tutorialChapterByID(chapterID)
	if chapter == nil {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial chapter not found")
	}
	id, gameID := s.nextTutorialIDs()
	g, _, err := s.buildTutorialGame(chapter, gameID)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	s.store.Put(g)
	s.roomMu.Lock()
	s.tutorials[id] = &tutorialSession{ID: id, GameID: gameID, ChapterID: chapter.ID}
	session := *s.tutorials[id]
	s.roomMu.Unlock()
	view, err := s.tutorialView(&session, g)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	return g, view, nil
}

func (s *Service) TutorialState(sessionID string) (*model.Game, model.TutorialView, error) {
	session, err := s.tutorialSession(sessionID)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	ref, ok := s.store.Get(session.GameID)
	if !ok {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	view, err := s.tutorialView(&session, ref.Game)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	return ref.Game, view, nil
}

func (s *Service) TutorialActions(sessionID string) ([]model.LegalAction, int, error) {
	session, err := s.tutorialSession(sessionID)
	if err != nil {
		return nil, 0, err
	}
	ref, ok := s.store.Get(session.GameID)
	if !ok {
		return nil, 0, fmt.Errorf("tutorial game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if session.Completed {
		return []model.LegalAction{}, ref.Game.EventSeq, nil
	}
	steps, err := s.tutorialSteps(&session, ref.Game)
	if err != nil {
		return nil, ref.Game.EventSeq, err
	}
	if session.StepIndex >= 0 && session.StepIndex < len(steps) && steps[session.StepIndex].ActionType == model.ActionTutorialContinue {
		return []model.LegalAction{{Type: model.ActionTutorialContinue, Description: "continue tutorial"}}, ref.Game.EventSeq, nil
	}
	acts, err := s.engine.LegalActions(ref.Game, tutorialPlayerID)
	if err != nil {
		return nil, ref.Game.EventSeq, err
	}
	return acts, ref.Game.EventSeq, nil
}

func (s *Service) ApplyTutorialAction(sessionID string, action model.Action) (*model.Game, model.TutorialView, error) {
	session, err := s.tutorialSession(sessionID)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	ref, ok := s.store.Get(session.GameID)
	if !ok {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial game not found")
	}
	ref.Mu.Lock()
	defer ref.Mu.Unlock()
	if session.Completed {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial chapter is complete")
	}
	steps, err := s.tutorialSteps(&session, ref.Game)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	if session.StepIndex < 0 || session.StepIndex >= len(steps) {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial step not found")
	}
	step := steps[session.StepIndex]
	action.PlayerID = tutorialPlayerID
	action.GameID = session.GameID
	if step.ActionType != "" && action.Type != step.ActionType {
		return nil, model.TutorialView{}, fmt.Errorf("请按教学引导选择当前目标行动")
	}
	if step.Validate != nil && !step.Validate(action) {
		return nil, model.TutorialView{}, fmt.Errorf("请按教学引导选择目标")
	}
	if action.Type != model.ActionTutorialContinue {
		if err := s.engine.ApplyAction(ref.Game, action); err != nil {
			return nil, model.TutorialView{}, err
		}
	}
	if step.After != nil {
		if err := step.After(s, ref.Game); err != nil {
			return nil, model.TutorialView{}, err
		}
	}
	s.addTutorialNote(ref.Game, "教学提示", step.Title+" 已完成。")

	s.roomMu.Lock()
	latest := s.tutorials[session.ID]
	if latest == nil {
		s.roomMu.Unlock()
		return nil, model.TutorialView{}, fmt.Errorf("tutorial session not found")
	}
	latest.StepIndex++
	if latest.StepIndex >= len(steps) {
		latest.Completed = true
	}
	session = *latest
	s.roomMu.Unlock()

	view, err := s.tutorialView(&session, ref.Game)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	return ref.Game, view, nil
}

func (s *Service) RestartTutorial(sessionID string) (*model.Game, model.TutorialView, error) {
	session, err := s.tutorialSession(sessionID)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	chapter := tutorialChapterByID(session.ChapterID)
	if chapter == nil {
		return nil, model.TutorialView{}, fmt.Errorf("tutorial chapter not found")
	}
	g, _, err := s.buildTutorialGame(chapter, session.GameID)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	ref, ok := s.store.Get(session.GameID)
	if !ok {
		s.store.Put(g)
	} else {
		ref.Mu.Lock()
		ref.Game = g
		ref.Mu.Unlock()
	}
	s.roomMu.Lock()
	if latest := s.tutorials[session.ID]; latest != nil {
		latest.StepIndex = 0
		latest.Completed = false
		session = *latest
	}
	s.roomMu.Unlock()
	view, err := s.tutorialView(&session, g)
	if err != nil {
		return nil, model.TutorialView{}, err
	}
	return g, view, nil
}

func (s *Service) nextTutorialIDs() (string, string) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	id := fmt.Sprintf("tutorial-%d", s.nextTutorial)
	gameID := fmt.Sprintf("tutorial-game-%d", s.nextTutorialGame)
	s.nextTutorial++
	s.nextTutorialGame++
	return id, gameID
}

func (s *Service) tutorialGameIDs() map[string]bool {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	ids := map[string]bool{}
	for _, session := range s.tutorials {
		if session != nil && session.GameID != "" {
			ids[session.GameID] = true
		}
	}
	return ids
}

func (s *Service) tutorialSession(sessionID string) (tutorialSession, error) {
	s.roomMu.Lock()
	defer s.roomMu.Unlock()
	session := s.tutorials[sessionID]
	if session == nil {
		return tutorialSession{}, fmt.Errorf("tutorial session not found")
	}
	return *session, nil
}

func (s *Service) tutorialView(session *tutorialSession, g *model.Game) (model.TutorialView, error) {
	chapter := tutorialChapterByID(session.ChapterID)
	if chapter == nil {
		return model.TutorialView{}, fmt.Errorf("tutorial chapter not found")
	}
	steps, err := s.tutorialSteps(session, g)
	if err != nil {
		return model.TutorialView{}, err
	}
	index := tutorialChapterIndex(chapter.ID)
	view := model.TutorialView{
		SessionID:       session.ID,
		ChapterID:       chapter.ID,
		ChapterTitle:    chapter.Title,
		ChapterIndex:    index,
		ChapterCount:    len(tutorialChapters()),
		Completed:       session.Completed,
		Chapters:        tutorialChapterSummaries(),
		CompletionTitle: "本章完成",
		CompletionBody:  "这一章已经走完，可以进入下一章继续。",
	}
	if session.Completed && chapter.ID == "settlement_money" {
		view.CompletionTitle = "恭喜完成新手引导"
		view.CompletionBody = "你已经完成全部新手教学，并在这局获得第一名。现在可以查看最终结算，或回到大厅开始正式对局。"
	}
	if session.Completed && chapter.ID == "overview" {
		view.CompletionTitle = "完成概览"
		view.CompletionBody = "你已经看过一局游戏的大框架：先竞拍港务长，再出航、放置、航行、结算，最后按现金和股票计算胜负。下一章开始亲自操作竞拍与出航准备。"
	}
	if !session.Completed && session.StepIndex >= 0 && session.StepIndex < len(steps) {
		step := steps[session.StepIndex]
		view.StepID = step.ID
		view.StepTitle = step.Title
		view.Body = step.Body
		view.Target = step.Target
		view.Targets = append([]string{}, step.Targets...)
		view.AllowedAction = string(step.ActionType)
	}
	return view, nil
}

func (s *Service) tutorialStep(session *tutorialSession, g *model.Game) (tutorialStep, error) {
	steps, err := s.tutorialSteps(session, g)
	if err != nil {
		return tutorialStep{}, err
	}
	if session.StepIndex < 0 || session.StepIndex >= len(steps) {
		return tutorialStep{}, fmt.Errorf("tutorial step not found")
	}
	return steps[session.StepIndex], nil
}

func (s *Service) tutorialSteps(session *tutorialSession, g *model.Game) ([]tutorialStep, error) {
	chapter := tutorialChapterByID(session.ChapterID)
	if chapter == nil {
		return nil, fmt.Errorf("tutorial chapter not found")
	}
	steps, err := chapter.Build(s, g)
	if err != nil {
		return nil, err
	}
	return emphasizeTutorialStepBodies(steps), nil
}

func (s *Service) buildTutorialGame(chapter *tutorialChapter, gameID string) (*model.Game, []tutorialStep, error) {
	seed := chapter.Seed
	if seed == 0 {
		seed = 2026062501
	}
	g := rules.NewGame(gameID, seed)
	steps, err := chapter.Build(s, g)
	if err != nil {
		return nil, nil, err
	}
	steps = emphasizeTutorialStepBodies(steps)
	s.addTutorialNote(g, chapter.Title, chapter.Description)
	return g, steps, nil
}

func (s *Service) addTutorialNote(g *model.Game, title string, body string) {
	g.EventSeq++
	g.Events = append(g.Events, model.Event{
		Seq:     g.EventSeq,
		Type:    "TutorialNote",
		Message: title,
		Data: map[string]interface{}{
			"body": body,
		},
	})
}

func emphasizeTutorialStepBodies(steps []tutorialStep) []tutorialStep {
	for i := range steps {
		if steps[i].ActionType == model.ActionTutorialContinue {
			continue
		}
		for _, phrase := range tutorialEmphasisPhrases[steps[i].ID] {
			steps[i].Body = emphasizeTutorialPhrase(steps[i].Body, phrase)
		}
	}
	return steps
}

func emphasizeTutorialPhrase(body string, phrase string) string {
	if phrase == "" || strings.Contains(body, "**"+phrase+"**") {
		return body
	}
	return strings.Replace(body, phrase, "**"+phrase+"**", 1)
}

var tutorialEmphasisPhrases = map[string][]string{
	"opening-bid":                    {"把竞拍数字设为 1 并点击“出价”"},
	"raise-to-eight":                 {"把数字改成 8，然后点击“出价”"},
	"raise-to-eighteen":              {"把数字设为 18 并出价"},
	"pass-high-bid":                  {"请点击“放弃”"},
	"second-round-raise":             {"请把数字设为 8 并出价"},
	"second-round-win":               {"请出价 14"},
	"buy-share":                      {"请购买人参股份"},
	"select-goods":                   {"请点选人参、肉豆蔻和丝绸，再点击“确认出航”"},
	"set-starts":                     {"请把人参设为 5、肉豆蔻设为 4、丝绸设为 0，然后确认起点"},
	"place-ship":                     {"请把同伙放到人参船"},
	"place-port":                     {"把同伙放到港口 B"},
	"place-dock":                     {"请把同伙放到船坞 A"},
	"place-pirate":                   {"请放到海盗船第 1 格"},
	"place-insurance":                {"请把同伙放到保险商位置"},
	"place-small-navigator":          {"请把同伙放到小领航员位置"},
	"voyage-pirate":                  {"请把同伙放到海盗船第 1 格"},
	"voyage-ship":                    {"请把同伙放到肉豆蔻船"},
	"pirate-board":                   {"请让海盗船长登上丝绸船"},
	"place-small-navigator-voyage":   {"请把同伙放到小领航员"},
	"small-navigator-move":           {"把肉豆蔻船前进 1 格，人参船留在原位", "点击“移动”"},
	"second-round-pirate":            {"请把同伙放到海盗船第 1 格"},
	"place-big-navigator":            {"请把同伙放到大领航员位置"},
	"pirate-skip-board":              {"请点击“海盗留在船上”"},
	"second-round-ship":              {"请把同伙放到肉豆蔻船"},
	"big-navigator-move":             {"把人参船设为 +1、肉豆蔻船设为 -1", "点击“移动”"},
	"pirate-loot-captain":            {"把丝绸船送往港口"},
	"settlement-auction-bid":         {"先出价 12"},
	"settlement-auction-win":         {"请出价 20"},
	"settlement-skip-share":          {"请点击“跳过购买股份”"},
	"settlement-select-goods":        {"请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”"},
	"settlement-set-starts":          {"请把人参设为 5、肉豆蔻设为 2、丝绸设为 2"},
	"settlement-place-ship":          {"把同伙放到人参船"},
	"settlement-place-pirate":        {"把同伙放到海盗船长位置"},
	"settlement-place-insurance":     {"把同伙放到保险商"},
	"loot-to-port":                   {"先把肉豆蔻船送往港口"},
	"loot-to-dock":                   {"现在把丝绸船送往船坞"},
	"confirm-settlement":             {"请点击“确认本轮结算”"},
	"settlement-all-in-open":         {"请先出价 24"},
	"settlement-all-in-auction":      {"请出价 44"},
	"settlement-all-in-skip-share":   {"请点击“跳过购买股份”"},
	"settlement-all-in-select-goods": {"请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”"},
	"settlement-all-in-set-starts":   {"请把人参设为 5、肉豆蔻设为 2、丝绸设为 2，然后确认起点"},
	"auto-mortgage-pirate":           {"请把同伙放到海盗船长位置"},
	"insurance-loss-ship":            {"把同伙放到丝绸船"},
	"insurance-loss-insurance":       {"再放到保险商"},
	"confirm-loss-settlement":        {"请点击“确认本轮结算”"},
	"bankruptcy-auction-pass":        {"请点击“放弃”"},
	"stowaway":                       {"请点肉豆蔻船"},
	"stowaway-tied-lowest":           {"仍然点肉豆蔻船"},
	"stowaway-silk":                  {"请点丝绸船"},
	"confirm-stowaway-settlement":    {"请点击“确认本轮结算”"},
	"round4-auction-bid":             {"请出价 14"},
	"round4-buy-share":               {"请购买 1 张人参股份"},
	"round4-select-goods":            {"请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”"},
	"round4-set-starts":              {"请把人参设为 4、肉豆蔻设为 0、丝绸设为 5，然后确认起点"},
	"round4-place-silk":              {"先把同伙放到丝绸船"},
	"round4-place-pirate":            {"现在把同伙放到海盗船长位置"},
	"round4-place-port":              {"现在放到港口 A"},
	"round4-loot-ginseng":            {"把人参船送往港口"},
	"confirm-round4-profit":          {"请点击“确认本轮结算”"},
	"round5-auction-bid":             {"请先出价 8"},
	"round5-auction-win":             {"请出价 16"},
	"round5-buy-share":               {"请购买 1 张人参股份"},
	"round5-select-goods":            {"请选择人参、丝绸和玉石出航，再点击“确认出航”"},
	"round5-set-starts":              {"请把人参设为 5、丝绸设为 4、玉石设为 0，然后确认起点"},
	"round5-place-ginseng":           {"先把同伙放到人参船"},
	"round5-place-silk":              {"把同伙放到丝绸船"},
	"round5-place-port":              {"把同伙放到港口 B"},
}

func tutorialActionButtonTarget(actionType model.ActionType, payload string) string {
	return fmt.Sprintf(`[data-action-key="%s:%s"]`, actionType, payload)
}

func tutorialPlaceButtonTarget(positionType string, target interface{}) string {
	targetValue := fmt.Sprint(target)
	if _, ok := target.(string); ok {
		targetValue = fmt.Sprintf(`\"%s\"`, target)
	}
	return tutorialActionButtonTarget(model.ActionPlaceAccomplice, fmt.Sprintf(`{\"positionType\":\"%s\",\"targetId\":%s}`, positionType, targetValue))
}

func tutorialStowawayButtonTarget(shipID int) string {
	return tutorialActionButtonTarget(model.ActionPlaceAccomplice, fmt.Sprintf(`{\"isStowaway\":true,\"occupiesSlot\":true,\"positionType\":\"ship\",\"targetId\":%d}`, shipID))
}

func tutorialPirateBoardButtonTarget(shipID int) string {
	return tutorialActionButtonTarget(model.ActionPirateBoard, fmt.Sprintf(`{\"shipId\":%d}`, shipID))
}

func tutorialPirateDestinationButtonTarget(shipID int, destination string) string {
	return tutorialActionButtonTarget(model.ActionPirateChooseDestination, fmt.Sprintf(`{\"destination\":\"%s\",\"shipId\":%d}`, destination, shipID))
}

func tutorialContinueButtonTarget() string {
	return tutorialActionButtonTarget(model.ActionTutorialContinue, `{}`)
}

func tutorialLegalCandidateAllowed(step tutorialStep, act model.LegalAction) bool {
	if step.Validate == nil {
		return true
	}
	switch act.Type {
	case model.ActionBid, model.ActionSelectGoods, model.ActionSetShipStarts, model.ActionNavigatorMove, model.ActionConfirmRound, model.ActionTutorialContinue:
		return true
	default:
		return step.Validate(model.Action{PlayerID: tutorialPlayerID, Type: act.Type, Payload: copyPayload(act.Payload)})
	}
}

func tutorialChapters() []tutorialChapter {
	return []tutorialChapter{
		overviewTutorialChapter(),
		introTutorialChapter(),
		placementTutorialChapter(),
		voyageTutorialChapter(),
		settlementTutorialChapterV2(),
	}
}

func overviewTutorialChapter() tutorialChapter {
	return tutorialChapter{
		ID:          "overview",
		Title:       "新手概览",
		Description: "先看完一局演示，了解游戏目标、每轮流程和终局结算。",
		Seed:        2026062601,
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			return []tutorialStep{
				{
					ID:         "overview-goal",
					Title:      "游戏目标",
					Body:       "这是一场 4 人竞争游戏。玩家通过竞拍港务长、购买股份、派同伙押注航行结果来赚取现金。游戏结束时，最终资产最高的人获胜；最终资产来自现金、股票价值，并扣除未还贷款。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return ensureStarted(s, g)
					},
				},
				{
					ID:         "overview-auction",
					Title:      "竞拍港务长",
					Body:       "现在进入竞拍阶段。每轮先竞拍港务长，出价最高的玩家成为本轮港务长；港务长之后会负责买股、选出航货物和设置货船起点。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewAuctionToPreparation(s, g)
					},
				},
				{
					ID:         "overview-preparation",
					Title:      "出航准备",
					Body:       "竞拍结束后进入出航准备。港务长每轮最多买 1 张公开股份，然后选择 3 种货物出航，并设置 3 艘货船的起点；三个起点数字加起来必须等于 9。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewPreparationToPlacement(s, g)
					},
				},
				{
					ID:         "overview-placement",
					Title:      "放置同伙",
					Body:       "出航准备完成后进入放置阶段。每名玩家每轮有 3 个同伙，把同伙放到货船、港口、船坞、海盗、领航员或保险商，就是押不同的航行结果。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewToFirstSailing(s, g)
					},
				},
				{
					ID:         "overview-first-sailing",
					Title:      "第一次航行",
					Body:       "第一轮放置后，货船开始航行。每艘出航货船都会独立随机前进 1-6 步；船的位置会影响它到港、进船坞或触发海盗事件的可能性。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewToRoundReview(s, g)
					},
				},
				{
					ID:         "overview-events",
					Title:      "航行事件",
					Body:       "一轮航行中可能遇到海盗、领航员、保险商赔付、货船到港或进船坞等事件。这里先知道它们都属于航行过程；后续章节会逐步让你亲自处理。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "overview-settlement",
					Title:      "本轮结算",
					Body:       "航行结束后进入结算。货船、港口、船坞、海盗和保险商会按规则依次结算；押中的位置获得现金，没押中的位置通常没有收益。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "overview-market",
					Title:      "货物涨价",
					Body:       "结算时，成功到港的货物股价会上涨。股票最终会按对应货物的当前市值计入资产，所以货物能否到港会影响长期得分。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewToMidgame(s, g)
					},
				},
				{
					ID:         "overview-midgame",
					Title:      "多轮重复",
					Body:       "现在已经推进过几轮。每轮都会重复竞拍、出航准备、放置同伙、航行和结算，但现金、股票价格和船位会不断改变局面。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						return scriptOverviewToGameEnd(s, g)
					},
				},
				{
					ID:         "overview-game-end",
					Title:      "游戏结束判定",
					Body:       "游戏规定：当任意一种股票的价格达到 30 时，当前回合结算完成后游戏结束。现在已经有货物股价到达 30，因此这局进入终局结算。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "overview-final-score",
					Title:      "终局资产结算",
					Body:       "终局资产 = 现金 + 股票价值 - 抵押股票赎金。现金来自航行中的收益，股票价值来自持有股份和当前股价，未还贷款会在最后扣分。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
			}, nil
		},
	}
}

func introTutorialChapter() tutorialChapter {
	return tutorialChapter{
		ID:          "intro",
		Title:       "竞拍与出航准备",
		Description: "练习竞拍、买股、选货和设置货船起点。",
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			if err := ensureStarted(s, g); err != nil {
				return nil, err
			}
			return []tutorialStep{
				{
					ID:         "opening-bid",
					Title:      "参与竞拍",
					Body:       "每轮先竞拍港务长。港务长之后会买股、选货物、设置货船起点。现在把竞拍数字设为 1 并点击“出价”；只有最后赢得竞拍的人才会实际付款。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(1),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 5}},
							model.Action{PlayerID: 3, Type: model.ActionPassBid},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第一次加价", "P2 把价格加到 5，P3 和 P4 放弃。现在只剩你和 P2；如果继续竞拍，必须出更高的价格。")
						return nil
					},
				},
				{
					ID:         "raise-to-eight",
					Title:      "继续出价",
					Body:       "当前最高价是 P2 的 5。继续竞争港务长必须出 6 或更高；这一步把数字改成 8，然后点击“出价”。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(8),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 15}},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "再次被超过", "P2 没有放弃，而是把价格加到 15。竞拍会在还没放弃的玩家之间继续轮流。")
						return nil
					},
				},
				{
					ID:         "raise-to-eighteen",
					Title:      "再次出价",
					Body:       "当前最高价是 P2 的 15。继续竞拍必须出更高的价格；这一步把数字设为 18 并出价。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(18),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 25}},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "价格变高", "P2 继续加到 25。这个价已经不低了，先放弃一次。")
						return nil
					},
				},
				{
					ID:         "pass-high-bid",
					Title:      "放弃竞拍",
					Body:       "现在价格已经很高。请点击“放弃”；放弃后，本轮竞拍不能再回来出价。",
					Target:     "#bidPassBtn",
					ActionType: model.ActionPassBid,
					Validate:   validateType(model.ActionPassBid),
					After: func(s *Service, g *model.Game) error {
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 1}},
							model.Action{PlayerID: 3, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 3}},
							model.Action{PlayerID: 4, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 5}},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "进入第 2 轮", "P2 成为港务长后，完成了买股、选货和设置起点。教学随后自动推进了放置、航行和结算；这些操作会在后续章节逐步学习。第 2 轮开始后，P2 出价 1，P3 加到 3，P4 加到 5，现在轮到你。")
						return nil
					},
				},
				{
					ID:         "second-round-raise",
					Title:      "再次参与竞拍",
					Body:       "第二轮重新竞拍。当前最高价是 P4 的 5；请把数字设为 8 并出价，继续留在竞拍中。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(8),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPassBid},
							model.Action{PlayerID: 3, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 11}},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第二轮继续", "P2 放弃，P3 加到 11，P4 放弃。现在只剩你和 P3。")
						return nil
					},
				},
				{
					ID:         "second-round-win",
					Title:      "赢得港务长竞拍",
					Body:       "当前最高价是 P3 的 11。请出价 14；如果你最后成为港务长，就要支付最终竞拍价。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(14),
					After: func(s *Service, g *model.Game) error {
						for g.Phase == model.PhaseAuction {
							if g.CurrentPlayer == tutorialPlayerID {
								return fmt.Errorf("auction returned to tutorial player before ending")
							}
							if err := s.engine.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionPassBid}); err != nil {
								return err
							}
						}
						return nil
					},
				},
				{
					ID:         "buy-share",
					Title:      "购买一张股份",
					Body:       "港务长每轮最多可以买 1 张公开股份，也可以不买。当前货物市值是 0，但最低购买价是 5；请购买人参股份，之后它会按人参市值计入资产。",
					Target:     `[data-goods-id="1"]`,
					Targets:    []string{`[data-goods-id="1"]`, tutorialActionButtonTarget(model.ActionBuyShare, `{\"goodsId\":1}`)},
					ActionType: model.ActionBuyShare,
					Validate:   validatePayload("goodsId", 1),
				},
				{
					ID:         "select-goods",
					Title:      "选择 3 种出航货物",
					Body:       "每轮只有 3 种货物出航。剩下 1 种本轮不上船、不掷骰、不升值。请点选人参、肉豆蔻和丝绸，再点击“确认出航”。",
					Target:     "#selectGoodsSubmitBtn",
					Targets:    []string{`[data-goods-id="1"]`, `[data-goods-id="2"]`, `[data-goods-id="3"]`, "#selectGoodsSubmitBtn"},
					ActionType: model.ActionSelectGoods,
					Validate:   validateGoodsList(1, 2, 3),
				},
				{
					ID:         "set-starts",
					Title:      "设置货船起点",
					Body:       "3 艘货船起点都必须在 0 到 5 之间，且总和必须等于 9。请把人参设为 5、肉豆蔻设为 4、丝绸设为 0，然后确认起点。",
					Target:     "#startsSubmitBtn",
					Targets:    []string{`.ship-start-input[data-ship-id="1"]`, `.ship-start-input[data-ship-id="2"]`, `.ship-start-input[data-ship-id="3"]`, "#startsSubmitBtn"},
					ActionType: model.ActionSetShipStarts,
					Validate:   validateStarts(map[string]int{"1": 5, "2": 4, "3": 0}),
				},
			}, nil
		},
	}
}

func placementTutorialChapter() tutorialChapter {
	return tutorialChapter{
		ID:          "placement_main",
		Title:       "放置同伙",
		Description: "练习把同伙放到货船、港口、船坞、海盗、保险和小领航员。",
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			if g.Status == model.StatusNotStarted {
				if err := setupPlayerPlacementRound(s, g, false, map[string]int{"1": 5, "2": 4, "3": 0}); err != nil {
					return nil, err
				}
			}
			return []tutorialStep{
				{
					ID:         "place-ship",
					Title:      "第 1 次放置：货船",
					Body:       "货船位置押注这艘船会到港。现在人参船起点靠前，当前更容易到港。请把同伙放到人参船，并支付当前最低费用。",
					Target:     `.ship[data-ship-id="1"]`,
					Targets:    []string{`.ship[data-ship-id="1"]`, tutorialPlaceButtonTarget("ship", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 1),
					After: func(s *Service, g *model.Game) error {
						if err := scriptUntilPlacementTurn(s, g, 2); err != nil {
							return err
						}
						s.addTutorialNote(g, "进入第 2 次放置", "系统玩家完成了各自的放置，教学已自动推进第 1 次航行。第二章先关注放置位置的含义；航行、海盗和结算会在后续章节学习。现在轮到你第 2 次放置。")
						return nil
					},
				},
				{
					ID:         "place-port",
					Title:      "第 2 次放置：港口",
					Body:       "港口押注有船成功到港。港口 A、B、C 分别要至少 1、2、3 艘船到港才会结算。现在有两艘船位置靠前，把同伙放到港口 B。",
					Target:     `.port-overlay .board-slot[data-slot-id="B"]`,
					Targets:    []string{`.port-overlay .board-slot[data-slot-id="B"]`, tutorialPlaceButtonTarget("port", "B")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("port", "B"),
					After: func(s *Service, g *model.Game) error {
						if err := scriptUntilPlacementTurn(s, g, 3); err != nil {
							return err
						}
						s.addTutorialNote(g, "进入第 3 次放置", "系统玩家完成了各自的放置，教学已自动推进第 2 次航行。你本轮还剩最后 1 个同伙。")
						return nil
					},
				},
				{
					ID:         "place-dock",
					Title:      "第 3 次放置：船坞",
					Body:       "船坞押注有船没能到港。船坞 A、B、C 分别要至少 1、2、3 艘船未到港才会结算。丝绸船当前靠后，到港概率低，请把同伙放到船坞 A。",
					Target:     `.dock-overlay .board-slot[data-slot-id="A"]`,
					Targets:    []string{`.dock-overlay .board-slot[data-slot-id="A"]`, tutorialPlaceButtonTarget("dock", "A")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("dock", "A"),
					After: func(s *Service, g *model.Game) error {
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := setupPlayerPlacementRoundFromAuction(s, g, false, map[string]int{"1": 5, "2": 4, "3": 0}); err != nil {
							return err
						}
						s.addTutorialNote(g, "进入第 2 轮", "第一轮的 3 个同伙已经用完，本轮剩余流程已自动推进完成。新一轮开始后，你会继续练习特殊位置。")
						return nil
					},
				},
				{
					ID:         "placement-round-break",
					Title:      "放置结束",
					Body:       "第 1 轮剩余流程已自动推进完成：人参和肉豆蔻到港，丝绸进船坞；你放过的货船、港口和船坞位置会按这些结果结算。新一轮开始后，每位玩家又有 3 个同伙可放。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "place-pirate",
					Title:      "第 1 次放置：海盗船",
					Body:       "新一轮开始后，同伙数量恢复。海盗船每格费用 5，先放的是船长，后放的是副手。请放到海盗船第 1 格。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						if err := scriptUntilPlacementTurn(s, g, 2); err != nil {
							return err
						}
						s.addTutorialNote(g, "海盗留在船上", "系统玩家完成了各自的放置，教学已自动推进第 1 次航行。这里先认识海盗船位置，海盗事件会在第三章实际处理。")
						return nil
					},
				},
				{
					ID:         "place-insurance",
					Title:      "第 2 次放置：保险商",
					Body:       "保险商不付放置费，立刻拿 10 比索；但结算时要优先赔付船坞收益。请把同伙放到保险商位置。",
					Target:     `.insurance-overlay .board-slot[data-slot-id="insurance"]`,
					Targets:    []string{`.insurance-overlay .board-slot[data-slot-id="insurance"]`, tutorialPlaceButtonTarget("insurance", "insurance")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("insurance", "insurance"),
					After: func(s *Service, g *model.Game) error {
						if err := scriptUntilPlacementTurn(s, g, 3); err != nil {
							return err
						}
						s.addTutorialNote(g, "保险商已经生效", "保险商放上去后立即拿到 10 比索。系统玩家完成了各自的放置，教学已自动推进航行；现在轮到你本轮第 3 次放置。")
						return nil
					},
				},
				{
					ID:         "place-small-navigator",
					Title:      "第 3 次放置：小领航员",
					Body:       "小领航员费用 2，会在第 3 次掷骰前行动，可让 1 艘未到港的船前进或后退 1 格。请把同伙放到小领航员位置。",
					Target:     `.navigator-overlay .board-slot[data-slot-id="small"]`,
					Targets:    []string{`.navigator-overlay .board-slot[data-slot-id="small"]`, tutorialPlaceButtonTarget("navigatorSmall", "small")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("navigatorSmall", "small"),
					After: func(s *Service, g *model.Game) error {
						if err := scriptUntilPhaseForPlayer(s, g, model.PhaseNavigatorAction); err != nil {
							return err
						}
						if err := s.engine.ApplyAction(g, model.Action{
							PlayerID: tutorialPlayerID,
							Type:     model.ActionNavigatorMove,
							Payload: map[string]interface{}{
								"moves": []interface{}{map[string]interface{}{"shipId": 3, "delta": 1}},
							},
						}); err != nil {
							return err
						}
						if err := scriptUntil(s, g, 80, func(g *model.Game) bool {
							return g.Phase == model.PhaseRoundReview
						}); err != nil {
							return err
						}
						s.addTutorialNote(g, "本轮收尾", "小领航员行动已由教学自动处理。本章先认识这个位置，具体怎么移动货船会在第三章学习。")
						return nil
					},
				},
			}, nil
		},
	}
}

func voyageTutorialChapter() tutorialChapter {
	return tutorialChapter{
		ID:          "voyage",
		Title:       "航行、领航和海盗",
		Description: "练习掷骰移动、领航、海盗登船和劫掠。",
		Seed:        2026062503,
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			if g.Status == model.StatusNotStarted {
				if err := setupPlayerPlacementRound(s, g, false, map[string]int{"1": 3, "2": 3, "3": 3}); err != nil {
					return nil, err
				}
			}
			return []tutorialStep{
				{
					ID:         "voyage-pirate",
					Title:      "放置到海盗船",
					Body:       "航行时，每艘出航货船都会独立掷骰，分别随机前进 1 到 6 格。请把同伙放到海盗船第 1 格，成为海盗船长；船长会优先处理海盗相关选择。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						return scriptUntilPlacementTurn(s, g, 2)
					},
				},
				{
					ID:         "first-sail-break",
					Title:      "第 1 次航行",
					Body:       "系统玩家完成各自的放置后，三艘出航货船各自掷骰并前进。事件区会记录每艘船的点数；船位变化后，进入第 2 次放置。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "voyage-ship",
					Title:      "放置到肉豆蔻船",
					Body:       "请把同伙放到肉豆蔻船。系统玩家完成放置后会继续航行；如果出现需要你处理的事件，教学会停下来。",
					Target:     `.ship[data-ship-id="2"]`,
					Targets:    []string{`.ship[data-ship-id="2"]`, tutorialPlaceButtonTarget("ship", 2)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 2),
					After: func(s *Service, g *model.Game) error {
						return scriptUntilPhaseForPlayer(s, g, model.PhasePirateBoarding)
					},
				},
				{
					ID:         "second-sail-break",
					Title:      "第 2 次航行",
					Body:       "第 2 次航行后，丝绸船刚好停在 13。货船停在 13、船上还有空位，且海盗船上有人时，会先处理登船选择；登船由海盗船长先决定，人参和肉豆蔻这次没有触发登船。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "pirate-board",
					Title:      "海盗船长登上丝绸船",
					Body:       "第 2 次移动后，丝绸船刚好停在 13，且船上还有空位。你是海盗船长，所以先由你决定是否登船。请让海盗船长登上丝绸船；登船后这名同伙会跟船走，不再留在海盗船上。",
					Target:     `.ship[data-ship-id="3"]`,
					Targets:    []string{`.ship[data-ship-id="3"]`, tutorialPirateBoardButtonTarget(3)},
					ActionType: model.ActionPirateBoard,
					Validate:   validatePayload("shipId", 3),
				},
				{
					ID:         "place-small-navigator-voyage",
					Title:      "放置小领航员",
					Body:       "登船选择结束后会进入第 3 次放置。请把同伙放到小领航员；它会在第 3 次掷骰前行动。",
					Target:     `.navigator-overlay .board-slot[data-slot-id="small"]`,
					Targets:    []string{`.navigator-overlay .board-slot[data-slot-id="small"]`, tutorialPlaceButtonTarget("navigatorSmall", "small")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("navigatorSmall", "small"),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g, model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}}); err != nil {
							return err
						}
						s.addTutorialNote(g, "系统玩家补上海盗副手", "你作为船长已经登上货船，海盗船上暂时没有船长。系统玩家 P2 放到了海盗副手位置。")
						return scriptUntilPhaseForPlayer(s, g, model.PhaseNavigatorAction)
					},
				},
				{
					ID:         "mate-leads-break",
					Title:      "海盗船上只剩副手",
					Body:       "现在海盗船上留下的是 P2 的副手。你登船后会跟着货船走，不再留在海盗船上。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "small-navigator-move",
					Title:      "执行小领航员行动",
					Body:       "小领航最多移动 1 格，也可以不移动。这里把肉豆蔻船前进 1 格，人参船留在原位，然后点击“移动”。领航员把船移动到 13 不会触发海盗；海盗只会在掷骰移动后按规则触发。",
					Target:     "#navSubmitBtn",
					Targets:    []string{`.nav-move-input[data-ship-id="2"]`, "#navSubmitBtn"},
					ActionType: model.ActionNavigatorMove,
					Validate:   validateMoves(map[int]int{2: 1}),
				},
				{
					ID:         "mate-decision-break",
					Title:      "副手代替船长处理劫掠",
					Body:       "刚才先执行小领航员，再进行第 3 次掷骰。第 3 次移动后，人参船刚好停在 13，发生海盗劫掠。你已经离开海盗船登上货船，因此这次劫掠由仍在海盗船上的副手处理。你不会参与这次决策和分钱。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
					After: func(s *Service, g *model.Game) error {
						if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionPirateChooseDestination, Payload: map[string]interface{}{"shipId": 1, "destination": "port"}}); err != nil {
							return err
						}
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := setupPlayerPlacementRoundFromAuction(s, g, false, map[string]int{"1": 3, "2": 3, "3": 3}); err != nil {
							return err
						}
						s.addTutorialNote(g, "进入下一轮", "本轮剩余结算和下一轮准备已自动推进完成。你已经看过这次事件的关键规则，后面继续练习另一种处理方式。")
						return nil
					},
				},
				{
					ID:         "second-round-pirate",
					Title:      "放置到海盗船",
					Body:       "新一轮重新开始放置。请把同伙放到海盗船第 1 格，继续作为海盗船长。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g, model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}}); err != nil {
							return err
						}
						s.addTutorialNote(g, "副手也在海盗船上", "P2 放到了海盗副手位置。船长和副手都留在海盗船上时，由船长决定劫掠去向，结算时两人平分海盗收益。")
						return scriptUntilPlacementTurn(s, g, 2)
					},
				},
				{
					ID:         "place-big-navigator",
					Title:      "放置大领航员",
					Body:       "这次放置大领航员。请把同伙放到大领航员位置；它会在第 3 次掷骰前行动，总移动量最多 2。",
					Target:     `.navigator-overlay .board-slot[data-slot-id="big"]`,
					Targets:    []string{`.navigator-overlay .board-slot[data-slot-id="big"]`, tutorialPlaceButtonTarget("navigatorBig", "big")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("navigatorBig", "big"),
					After: func(s *Service, g *model.Game) error {
						return scriptUntilPhaseForPlayer(s, g, model.PhasePirateBoarding)
					},
				},
				{
					ID:         "second-round-sail-break",
					Title:      "第 2 次航行",
					Body:       "这一轮第 2 次航行后，又有货船停在 13，且船上还有空位。登船选择仍然由海盗船长先决定；这次你留在海盗船上，后面如果发生劫掠，船长才会参与决策。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "pirate-skip-board",
					Title:      "选择留在海盗船上",
					Body:       "第 2 次移动后再次出现可登船的目标。这次请点击“海盗留在船上”，让你作为海盗船长留在海盗船上。",
					Target:     tutorialActionButtonTarget(model.ActionPirateSkipBoard, `{}`),
					ActionType: model.ActionPirateSkipBoard,
					Validate:   validateType(model.ActionPirateSkipBoard),
					After: func(s *Service, g *model.Game) error {
						if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionPirateSkipBoard}); err != nil {
							return err
						}
						s.addTutorialNote(g, "船长和副手都留在海盗船上", "你放弃登船后，系统玩家 P2 的副手也放弃登船。你仍然是海盗船长。")
						return nil
					},
				},
				{
					ID:         "second-round-ship",
					Title:      "放置到肉豆蔻船",
					Body:       "现在进入第 3 次放置。请把同伙放到肉豆蔻船；放置完成后按当前规则继续航行。",
					Target:     `.ship[data-ship-id="2"]`,
					Targets:    []string{`.ship[data-ship-id="2"]`, tutorialPlaceButtonTarget("ship", 2)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 2),
					After: func(s *Service, g *model.Game) error {
						return scriptUntilPhaseForPlayer(s, g, model.PhaseNavigatorAction)
					},
				},
				{
					ID:         "big-navigator-move",
					Title:      "执行大领航员行动",
					Body:       "大领航总移动量最多 2，可以分给不同船，也可以不移动。把人参船设为 +1、肉豆蔻船设为 -1，然后点击“移动”。领航员把船移动到 13 不会触发海盗；海盗只会在掷骰移动后按规则触发。",
					Target:     "#navSubmitBtn",
					Targets:    []string{`.nav-move-input[data-ship-id="1"]`, `.nav-move-input[data-ship-id="2"]`, "#navSubmitBtn"},
					ActionType: model.ActionNavigatorMove,
					Validate:   validateMoves(map[int]int{1: 1, 2: -1}),
				},
				{
					ID:         "pirate-loot-captain",
					Title:      "决定丝绸船去向",
					Body:       "刚才先执行大领航员，再进行第 3 次掷骰。第 3 次移动后，丝绸船刚好停在 13，再次发生劫掠。你和副手都留在海盗船上，所以由你作为船长选择去港口还是船坞；把丝绸船送往港口。",
					Target:     `.destination-zone.port`,
					Targets:    []string{`.destination-zone.port`, tutorialPirateDestinationButtonTarget(3, "port")},
					ActionType: model.ActionPirateChooseDestination,
					Validate:   validateDestination(3, "port"),
				},
				{
					ID:         "pirate-payout-break",
					Title:      "海盗收益平分",
					Body:       "劫掠处理后进入结算。事件区可以看到海盗收益来自被劫船，并由仍在海盗船上的船长和副手平分；登上货船的海盗不会参与这笔分配。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
			}, nil
		},
	}
}

func settlementTutorialChapter() tutorialChapter {
	return tutorialChapter{
		ID:          "settlement_money",
		Title:       "结算、股票和资金困难",
		Description: "练习结算、股票升值、自动抵押和常见破产偷渡。",
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			if g.Status == model.StatusNotStarted {
				if err := setupRoundReviewByScript(s, g); err != nil {
					return nil, err
				}
			}
			return []tutorialStep{
				{
					ID:         "confirm-settlement",
					Title:      "确认完整结算",
					Body:       "事件区记录了船上收益、港口收益、船坞收益、保险赔付和货物升值。本局里丝绸船进了船坞，保险商会先赔付船坞 A 的收益。成功到港的货物会升值，持有的股份最终会按市值计入资产。请点击“确认本轮结算”，进入下一轮。",
					Target:     tutorialActionButtonTarget(model.ActionConfirmRound, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionConfirmRound, `{}`)},
					ActionType: model.ActionConfirmRound,
					Validate:   validateType(model.ActionConfirmRound),
					After: func(s *Service, g *model.Game) error {
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := setupPlayerPlacementRoundFromAuction(s, g, false, map[string]int{"1": 3, "2": 3, "3": 3}); err != nil {
							return err
						}
						g.Players[1].Cash = 0
						g.Players[1].Shares[model.GoodsGinseng] = 1
						g.Players[1].MortgagedShareCount = 0
						s.addTutorialNote(g, "现金不足", "你现在现金为 0，但还有未抵押股份。现金不足且需要付款时，规则会先尝试抵押股份。")
						return nil
					},
				},
				{
					ID:         "settlement-money-break",
					Title:      "自动抵押说明",
					Body:       "刚才系统完成了下一轮准备，并把你的现金设为 0、保留 1 张未抵押股份。现金不足且需要付款时，规则会先尝试抵押股份。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "auto-mortgage-place",
					Title:      "自动抵押后放置",
					Body:       "放到人参船需要 1 比索。你现金不足，但还有未抵押股份；请放到人参船，系统会自动抵押 1 张股份获得 12 比索并支付费用。最终计分时每张抵押股份会扣 15。",
					Target:     `.ship[data-ship-id="1"]`,
					Targets:    []string{`.ship[data-ship-id="1"]`, tutorialPlaceButtonTarget("ship", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 1),
					After: func(s *Service, g *model.Game) error {
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := setupBankruptPlacementTurn(s, g); err != nil {
							return err
						}
						s.addTutorialNote(g, "破产状态", "你没有现金、没有可抵押股份，且没有 0 费位置可放。系统会让你进入本轮破产状态。")
						return nil
					},
				},
				{
					ID:         "bankruptcy-break",
					Title:      "准备偷渡上船",
					Body:       "刚才的放置已经通过自动抵押完成。现在你没有现金、没有可抵押股份，也没有 0 费普通位置可放，所以只能作为偷渡客上船。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "stowaway",
					Title:      "作为偷渡客上船",
					Body:       "现在没有现金、没有可抵押股份，也没有 0 费普通位置可放。破产时只能作为偷渡客上最低费用的货船；请放到人参船。极少见的无空位情况先不处理。",
					Target:     `.ship[data-ship-id="1"]`,
					Targets:    []string{`.ship[data-ship-id="1"]`, tutorialStowawayButtonTarget(1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 1),
				},
			}, nil
		},
	}
}

func settlementTutorialChapterV2() tutorialChapter {
	return tutorialChapter{
		ID:          "settlement_money",
		Title:       "结算、股票和资金困难",
		Description: "练习结算、股票升值、自动抵押和常见破产偷渡。",
		Seed:        1969,
		Build: func(s *Service, g *model.Game) ([]tutorialStep, error) {
			if g.Status == model.StatusNotStarted {
				if err := ensureStarted(s, g); err != nil {
					return nil, err
				}
			}
			return []tutorialStep{
				{
					ID:         "settlement-auction-bid",
					Title:      "竞拍港务长",
					Body:       "先出价 12 参与竞拍。港务长由最高出价者担任，系统玩家也会轮流出价或放弃。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(12),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 16}},
							model.Action{PlayerID: 3, Type: model.ActionPassBid},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第 1 轮竞拍", "P2 加到 16，P3 和 P4 放弃。现在只剩你和 P2；请继续决定是否拿下港务长。")
						return nil
					},
				},
				{
					ID:         "settlement-auction-win",
					Title:      "继续竞拍",
					Body:       "当前最高价是 16。请出价 20。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(20),
					After: func(s *Service, g *model.Game) error {
						return finishTutorialAuctionAfterBid(s, g)
					},
				},
				{
					ID:         "settlement-skip-share",
					Title:      "跳过购买股份",
					Body:       "港务长每轮可以买 1 张公开股份，也可以跳过。请点击“跳过购买股份”。",
					Target:     tutorialActionButtonTarget(model.ActionSkipBuyShare, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionSkipBuyShare, `{}`)},
					ActionType: model.ActionSkipBuyShare,
					Validate:   validateType(model.ActionSkipBuyShare),
				},
				{
					ID:         "settlement-select-goods",
					Title:      "选择本轮出航货物",
					Body:       "请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”。",
					Target:     "#selectGoodsSubmitBtn",
					Targets:    []string{`[data-goods-id="1"]`, `[data-goods-id="2"]`, `[data-goods-id="3"]`, "#selectGoodsSubmitBtn"},
					ActionType: model.ActionSelectGoods,
					Validate:   validateGoodsList(1, 2, 3),
				},
				{
					ID:         "settlement-set-starts",
					Title:      "设置本轮起点",
					Body:       "请把人参设为 5、肉豆蔻设为 2、丝绸设为 2，三艘船起点总和仍然必须等于 9。",
					Target:     "#startsSubmitBtn",
					Targets:    []string{`.ship-start-input[data-ship-id="1"]`, `.ship-start-input[data-ship-id="2"]`, `.ship-start-input[data-ship-id="3"]`, "#startsSubmitBtn"},
					ActionType: model.ActionSetShipStarts,
					Validate:   validateStarts(map[string]int{"1": 5, "2": 2, "3": 2}),
				},
				{
					ID:         "settlement-place-ship",
					Title:      "放置到人参船",
					Body:       "把同伙放到人参船。货船位置的收益要等航行结果出来后再结算。",
					Target:     `.ship[data-ship-id="1"]`,
					Targets:    []string{`.ship[data-ship-id="1"]`, tutorialPlaceButtonTarget("ship", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 1),
					After: func(s *Service, g *model.Game) error {
						return applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
						)
					},
				},
				{
					ID:         "settlement-place-pirate",
					Title:      "放置到海盗船",
					Body:       "把同伙放到海盗船长位置；海盗船长负责处理海盗相关选择。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						return applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}},
						)
					},
				},
				{
					ID:         "settlement-place-insurance",
					Title:      "放置到保险商",
					Body:       "把同伙放到保险商。保险商会立刻拿 10 比索；如果有船进入船坞，结算时要赔付对应船坞奖励。",
					Target:     `.insurance-overlay .board-slot[data-slot-id="insurance"]`,
					Targets:    []string{`.insurance-overlay .board-slot[data-slot-id="insurance"]`, tutorialPlaceButtonTarget("insurance", "insurance")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("insurance", "insurance"),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
						); err != nil {
							return err
						}
						if g.Phase != model.PhasePirateLooting || g.CurrentPlayer != tutorialPlayerID {
							return fmt.Errorf("expected pirate looting after settlement placements, got phase %s player %d", g.Phase, g.CurrentPlayer)
						}
						if len(g.Round.PendingLootShips) != 2 ||
							g.Round.PendingLootShips[0] != model.GoodsNutmeg ||
							g.Round.PendingLootShips[1] != model.GoodsSilk {
							return fmt.Errorf("expected nutmeg and silk pending pirate loot, got %+v", g.Round.PendingLootShips)
						}
						return nil
					},
				},
				{
					ID:         "settlement-looting-break",
					Title:      "进入海盗劫掠",
					Body:       "系统玩家完成各自的放置，教学自动推进后续航行。第 3 次移动后，肉豆蔻船和丝绸船停在 13，进入海盗劫掠；你是海盗船长，需要依次决定它们去港口还是船坞。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "loot-to-port",
					Title:      "决定肉豆蔻船去向",
					Body:       "两艘船在第 3 次移动后刚好停在 13，海盗船长要依次决定它们去港口还是船坞。先把肉豆蔻船送往港口；海盗的劫掠收益和去向无关，但送到港口的货物会在结算时涨价。",
					Target:     `.destination-zone.port`,
					Targets:    []string{`.destination-zone.port`, tutorialPirateDestinationButtonTarget(2, "port")},
					ActionType: model.ActionPirateChooseDestination,
					Validate:   validateDestination(2, "port"),
				},
				{
					ID:         "loot-to-dock",
					Title:      "决定丝绸船去向",
					Body:       "现在把丝绸船送往船坞。即使被海盗劫掠后送进船坞，海盗仍然正常拿劫掠收益；船坞上有人时，保险商还要赔付对应的船坞奖励。",
					Target:     `.destination-zone.dock`,
					Targets:    []string{`.destination-zone.dock`, tutorialPirateDestinationButtonTarget(3, "dock")},
					ActionType: model.ActionPirateChooseDestination,
					Validate:   validateDestination(3, "dock"),
				},
				{
					ID:         "settlement-ship-break",
					Title:      "货船收益按船上人数平分",
					Body:       "结算已经完成，先看事件区的货船收益。你和 P2 都在到港的人参船上，这艘船没有坐满也没关系，收益只按实际在船上的人平分：人参船收益 18，所以你和 P2 各拿 9。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "settlement-pirate-break",
					Title:      "海盗收益和副手平分",
					Body:       "再看海盗收益。你是船长，P2 是副手，两个人都还留在海盗船上，所以肉豆蔻和丝绸两艘被劫船的收益都由你们平分。如果海盗船上只有船长，没有副手，那么船长会独吞这笔收益。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "settlement-insurance-break",
					Title:      "保险商赔付船坞收益",
					Body:       "你本轮还做了保险商，放置时先拿到 10 比索；但丝绸船被你送进船坞 A，船坞 A 有 P4 的同伙。船坞收益应由保险商支付，因此你需要支付 6 比索给 P4。保险商赚不赚，要看有没有船进船坞以及要赔多少。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "settlement-market-break",
					Title:      "到港货物会涨价",
					Body:       "最后看货物涨价。正常到港的人参会涨价；被海盗劫掠后送往港口的肉豆蔻也会涨价；被送进船坞的丝绸不会涨价。你持有的股份最终会按对应货物市值计入资产，所以涨价会影响最终分数。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "confirm-settlement",
					Title:      "确认本轮结算",
					Body:       "请点击“确认本轮结算”。",
					Target:     tutorialActionButtonTarget(model.ActionConfirmRound, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionConfirmRound, `{}`)},
					ActionType: model.ActionConfirmRound,
					Validate:   validateType(model.ActionConfirmRound),
					After: func(s *Service, g *model.Game) error {
						return scriptToNextAuction(s, g)
					},
				},
				{
					ID:         "settlement-all-in-open",
					Title:      "参与竞拍",
					Body:       "当前竞拍重新开始。请先出价 24。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(24),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 36}},
							model.Action{PlayerID: 3, Type: model.ActionPassBid},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第 2 轮竞拍", "P2 加到 36，P3 和 P4 放弃。现在只剩你和 P2。")
						return nil
					},
				},
				{
					ID:         "settlement-all-in-auction",
					Title:      "继续竞拍",
					Body:       "当前最高价是 36。请出价 44。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(44),
					After: func(s *Service, g *model.Game) error {
						return finishTutorialAuctionAfterBid(s, g)
					},
				},
				{
					ID:         "settlement-all-in-skip-share",
					Title:      "跳过买股",
					Body:       "你已经拍下港务长。这里先不购买股份，请点击“跳过购买股份”。",
					Target:     tutorialActionButtonTarget(model.ActionSkipBuyShare, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionSkipBuyShare, `{}`)},
					ActionType: model.ActionSkipBuyShare,
					Validate:   validateType(model.ActionSkipBuyShare),
				},
				{
					ID:         "settlement-all-in-select-goods",
					Title:      "选择本轮出航货物",
					Body:       "请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”。",
					Target:     "#selectGoodsSubmitBtn",
					Targets:    []string{`[data-goods-id="1"]`, `[data-goods-id="2"]`, `[data-goods-id="3"]`, "#selectGoodsSubmitBtn"},
					ActionType: model.ActionSelectGoods,
					Validate:   validateGoodsList(1, 2, 3),
				},
				{
					ID:         "settlement-all-in-set-starts",
					Title:      "设置本轮起点",
					Body:       "请把人参设为 5、肉豆蔻设为 2、丝绸设为 2，然后确认起点。",
					Target:     "#startsSubmitBtn",
					Targets:    []string{`.ship-start-input[data-ship-id="1"]`, `.ship-start-input[data-ship-id="2"]`, `.ship-start-input[data-ship-id="3"]`, "#startsSubmitBtn"},
					ActionType: model.ActionSetShipStarts,
					Validate:   validateStarts(map[string]int{"1": 5, "2": 2, "3": 2}),
					After: func(s *Service, g *model.Game) error {
						s.addTutorialNote(g, "现金不足", "你刚拍下港务长。现在现金为 0，但还有未抵押股份；现金不足且需要付款时，规则会先尝试抵押股份。")
						return nil
					},
				},
				{
					ID:         "auto-mortgage-pirate",
					Title:      "自动抵押后放置到海盗船",
					Body:       "请把同伙放到海盗船长位置。你不需要先找抵押按钮；支付 5 比索但现金不足时，规则会自动抵押 1 张股份。抵押会立刻给 12 比索，但最终计分时每张抵押股份会扣 15。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						return applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
						)
					},
				},
				{
					ID:         "insurance-loss-ship",
					Title:      "放置到丝绸船",
					Body:       "第 1 次移动后，把同伙放到丝绸船。前两个位置已经被系统玩家占了，你买的是第 3 格，费用正好是 5。",
					Target:     `.ship[data-ship-id="3"]`,
					Targets:    []string{`.ship[data-ship-id="3"]`, tutorialPlaceButtonTarget("ship", 3)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 3),
					After: func(s *Service, g *model.Game) error {
						return applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "C"}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
						)
					},
				},
				{
					ID:         "insurance-loss-insurance",
					Title:      "放置到保险商",
					Body:       "第 2 次移动后，再放到保险商。保险商会立刻拿 10 比索；结算时如果有船进入船坞，就按实际触发的船坞奖励赔付。",
					Target:     `.insurance-overlay .board-slot[data-slot-id="insurance"]`,
					Targets:    []string{`.insurance-overlay .board-slot[data-slot-id="insurance"]`, tutorialPlaceButtonTarget("insurance", "insurance")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("insurance", "insurance"),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}},
							model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "C"}},
							model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}},
						); err != nil {
							return err
						}
						if g.Phase != model.PhaseRoundReview {
							return fmt.Errorf("expected round review after insurance loss setup, got %s", g.Phase)
						}
						if g.Ships[model.GoodsGinseng].Status != model.ShipDocked ||
							g.Ships[model.GoodsNutmeg].Status != model.ShipDocked ||
							g.Ships[model.GoodsSilk].Status != model.ShipDocked {
							return fmt.Errorf("expected all ships docked, got ginseng=%s nutmeg=%s silk=%s", g.Ships[model.GoodsGinseng].Status, g.Ships[model.GoodsNutmeg].Status, g.Ships[model.GoodsSilk].Status)
						}
						return nil
					},
				},
				{
					ID:         "insurance-loss-break",
					Title:      "保险商赔付压力",
					Body:       "结算时，进入船坞的船会按船坞 A、B、C 的顺序结算；有同伙占据的船坞收益由保险商支付。保险商放置时先拿 10 比索，但本轮需要支付实际触发的船坞奖励。现金和可抵押股份都用完时，最低扣到 0，不会变成负数，差额由银行补足。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "confirm-loss-settlement",
					Title:      "确认本轮结算",
					Body:       "你现在已经没有现金，也没有可抵押股份。请点击“确认本轮结算”。",
					Target:     tutorialActionButtonTarget(model.ActionConfirmRound, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionConfirmRound, `{}`)},
					ActionType: model.ActionConfirmRound,
					Validate:   validateType(model.ActionConfirmRound),
					After: func(s *Service, g *model.Game) error {
						return scriptToNextAuction(s, g)
					},
				},
				{
					ID:         "bankruptcy-auction-pass",
					Title:      "放弃竞拍",
					Body:       "本轮不参与港务长竞拍，请点击“放弃”。",
					Target:     tutorialActionButtonTarget(model.ActionPassBid, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionPassBid, `{}`)},
					ActionType: model.ActionPassBid,
					Validate:   validateType(model.ActionPassBid),
					After: func(s *Service, g *model.Game) error {
						if err := setupBankruptPlacementAfterAuctionPass(s, g); err != nil {
							return err
						}
						s.addTutorialNote(g, "破产状态", "系统玩家接手港务长并完成出航准备。现在轮到你放置；你没有现金、没有可抵押股份，也没有 0 费普通位置可放，只能作为偷渡客上船。")
						return nil
					},
				},
				{
					ID:         "bankruptcy-break",
					Title:      "准备偷渡上船",
					Body:       "出航准备已经完成。你没有现金、没有可抵押股份，也没有 0 费普通位置可放，所以本轮轮到你放置时，只能作为偷渡客上船。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "stowaway",
					Title:      "作为偷渡客上船",
					Body:       "破产时不能自由挑船，只能按当前可用船位的费用来选：选费用最低的空位偷渡；如果几个空位费用一样，选排列更靠前的船。现在最低费用目标是肉豆蔻船，请点肉豆蔻船。",
					Target:     `.ship[data-ship-id="2"]`,
					Targets:    []string{`.ship[data-ship-id="2"]`, tutorialStowawayButtonTarget(2)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 2),
					After: func(s *Service, g *model.Game) error {
						return scriptBankruptStowawayToSecondPlacement(s, g)
					},
				},
				{
					ID:         "stowaway-tied-lowest",
					Title:      "同价时选排列靠前的船",
					Body:       "系统玩家补完了几步放置。现在肉豆蔻船和丝绸船都有同价的最低空位；同价时选排列更靠前的船，所以这一步仍然点肉豆蔻船。",
					Target:     `.ship[data-ship-id="2"]`,
					Targets:    []string{`.ship[data-ship-id="2"]`, tutorialStowawayButtonTarget(2)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 2),
					After: func(s *Service, g *model.Game) error {
						return scriptBankruptStowawayToThirdPlacement(s, g)
					},
				},
				{
					ID:         "stowaway-silk",
					Title:      "继续选择最低费用空位",
					Body:       "肉豆蔻船的下一个空位更贵了，现在最低费用空位轮到丝绸船。请点丝绸船，完成本轮最后一次偷渡。",
					Target:     `.ship[data-ship-id="3"]`,
					Targets:    []string{`.ship[data-ship-id="3"]`, tutorialStowawayButtonTarget(3)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 3),
					After: func(s *Service, g *model.Game) error {
						return scriptBankruptStowawayRoundToReview(s, g)
					},
				},
				{
					ID:         "stowaway-payout-break",
					Title:      "偷渡也按到港结果结算",
					Body:       "第三轮结算完成。人参、肉豆蔻和丝绸都到港了；你刚才按最低费用规则完成偷渡，所在货船到港后仍会参与船上分账。本轮你通过肉豆蔻船获得 24 比索、丝绸船获得 30 比索，重新有了现金。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "confirm-stowaway-settlement",
					Title:      "确认偷渡收益",
					Body:       "偷渡收益让你重新有了现金。请点击“确认本轮结算”。",
					Target:     tutorialActionButtonTarget(model.ActionConfirmRound, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionConfirmRound, `{}`)},
					ActionType: model.ActionConfirmRound,
					Validate:   validateType(model.ActionConfirmRound),
					After: func(s *Service, g *model.Game) error {
						if err := scriptToNextAuction(s, g); err != nil {
							return err
						}
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 6}},
							model.Action{PlayerID: 3, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 10}},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第 4 轮竞拍", "P2 出价 6，P3 加到 10，P4 放弃。现在轮到你。")
						return nil
					},
				},
				{
					ID:         "round4-auction-bid",
					Title:      "竞拍港务长",
					Body:       "当前最高价是 10。请出价 14 争港务长；竞拍会按最高有效出价结算。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(14),
					After: func(s *Service, g *model.Game) error {
						return finishTutorialAuctionAfterBid(s, g)
					},
				},
				{
					ID:         "round4-buy-share",
					Title:      "购买人参股份",
					Body:       "港务长每轮最多可以买 1 张公开股份。请购买 1 张人参股份；股份最终会按对应货物市值计入资产。",
					Target:     `[data-goods-id="1"]`,
					Targets:    []string{`[data-goods-id="1"]`, tutorialActionButtonTarget(model.ActionBuyShare, `{\"goodsId\":1}`)},
					ActionType: model.ActionBuyShare,
					Validate:   validatePayload("goodsId", 1),
				},
				{
					ID:         "round4-select-goods",
					Title:      "选择本轮出航货物",
					Body:       "请选择人参、肉豆蔻和丝绸出航，再点击“确认出航”。",
					Target:     "#selectGoodsSubmitBtn",
					Targets:    []string{`[data-goods-id="1"]`, `[data-goods-id="2"]`, `[data-goods-id="3"]`, "#selectGoodsSubmitBtn"},
					ActionType: model.ActionSelectGoods,
					Validate:   validateGoodsList(1, 2, 3),
				},
				{
					ID:         "round4-set-starts",
					Title:      "设置本轮起点",
					Body:       "请把人参设为 4、肉豆蔻设为 0、丝绸设为 5，然后确认起点。",
					Target:     "#startsSubmitBtn",
					Targets:    []string{`.ship-start-input[data-ship-id="1"]`, `.ship-start-input[data-ship-id="2"]`, `.ship-start-input[data-ship-id="3"]`, "#startsSubmitBtn"},
					ActionType: model.ActionSetShipStarts,
					Validate:   validateStarts(map[string]int{"1": 4, "2": 0, "3": 5}),
					After: func(s *Service, g *model.Game) error {
						s.addTutorialNote(g, "第 4 轮准备", "你买入 1 张人参股份，并完成本轮出航准备。轮到你放置同伙。")
						return nil
					},
				},
				{
					ID:         "round4-place-silk",
					Title:      "放置到丝绸船",
					Body:       "丝绸船起点靠前，船上位置的收益也很直观。先把同伙放到丝绸船；货船成功到港时，船上的同伙会参与船上收益分配。",
					Target:     `.ship[data-ship-id="3"]`,
					Targets:    []string{`.ship[data-ship-id="3"]`, tutorialPlaceButtonTarget("ship", 3)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 3),
					After: func(s *Service, g *model.Game) error {
						return scriptRound4ToSecondPlacement(s, g)
					},
				},
				{
					ID:         "round4-place-pirate",
					Title:      "放置到海盗船",
					Body:       "第 1 次移动后，船位已经变化。现在把同伙放到海盗船长位置；海盗船长在发生海盗事件时负责处理去向选择。",
					Target:     `.pirate-overlay .board-slot[data-slot-id="1"]`,
					Targets:    []string{`.pirate-overlay .board-slot[data-slot-id="1"]`, tutorialPlaceButtonTarget("pirate", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("pirate", 1),
					After: func(s *Service, g *model.Game) error {
						return scriptRound4ToThirdPlacement(s, g)
					},
				},
				{
					ID:         "round4-place-port",
					Title:      "放置到港口 A",
					Body:       "丝绸船已经到港。港口 A 的条件是至少一艘船到港；现在放到港口 A，付出 4 比索成本，结算时可以稳定拿到 6 比索奖励。",
					Target:     `.port-overlay .board-slot[data-slot-id="A"]`,
					Targets:    []string{`.port-overlay .board-slot[data-slot-id="A"]`, tutorialPlaceButtonTarget("port", "A")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("port", "A"),
					After: func(s *Service, g *model.Game) error {
						return scriptRound4ToLooting(s, g)
					},
				},
				{
					ID:         "round4-loot-ginseng",
					Title:      "决定人参船去向",
					Body:       "第 3 次移动后，人参船刚好停在 13。你是海盗船长，把人参船送往港口：海盗获得劫掠收益，人参仍会按到港货物升值。",
					Target:     `.destination-zone.port`,
					Targets:    []string{`.destination-zone.port`, tutorialPirateDestinationButtonTarget(1, "port")},
					ActionType: model.ActionPirateChooseDestination,
					Validate:   validateDestination(1, "port"),
				},
				{
					ID:         "round4-profit-break",
					Title:      "多种收益可以同时出现",
					Body:       "这一轮你拿到了海盗劫掠收益、丝绸船收益和港口 A 奖励；人参船被送进港口后也继续升值。不同位置的收益会在同一次结算里按顺序发生。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "confirm-round4-profit",
					Title:      "确认本轮结算",
					Body:       "请点击“确认本轮结算”。",
					Target:     tutorialActionButtonTarget(model.ActionConfirmRound, `{}`),
					Targets:    []string{tutorialActionButtonTarget(model.ActionConfirmRound, `{}`)},
					ActionType: model.ActionConfirmRound,
					Validate:   validateType(model.ActionConfirmRound),
					After: func(s *Service, g *model.Game) error {
						return scriptToNextAuction(s, g)
					},
				},
				{
					ID:         "round5-auction-bid",
					Title:      "竞拍港务长",
					Body:       "请先出价 8 参与竞拍。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(8),
					After: func(s *Service, g *model.Game) error {
						if err := applyScriptActions(s, g,
							model.Action{PlayerID: 2, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 12}},
							model.Action{PlayerID: 3, Type: model.ActionPassBid},
							model.Action{PlayerID: 4, Type: model.ActionPassBid},
						); err != nil {
							return err
						}
						s.addTutorialNote(g, "第 5 轮竞拍", "P2 加到 12，P3 和 P4 放弃。现在只剩你和 P2。")
						return nil
					},
				},
				{
					ID:         "round5-auction-win",
					Title:      "继续竞拍",
					Body:       "当前最高价是 12。请出价 16。",
					Target:     "#bidAmountInput",
					Targets:    []string{"#bidAmountInput", "#bidSubmitBtn"},
					ActionType: model.ActionBid,
					Validate:   validateBid(16),
					After: func(s *Service, g *model.Game) error {
						return finishTutorialAuctionAfterBid(s, g)
					},
				},
				{
					ID:         "round5-buy-share",
					Title:      "购买人参股份",
					Body:       "你是港务长，可以购买 1 张公开股份。请购买 1 张人参股份；股份最终会按对应货物市值计入资产。",
					Target:     `[data-goods-id="1"]`,
					Targets:    []string{`[data-goods-id="1"]`, tutorialActionButtonTarget(model.ActionBuyShare, `{\"goodsId\":1}`)},
					ActionType: model.ActionBuyShare,
					Validate:   validatePayload("goodsId", 1),
				},
				{
					ID:         "round5-select-goods",
					Title:      "选择本轮出航货物",
					Body:       "请选择人参、丝绸和玉石出航，再点击“确认出航”。",
					Target:     "#selectGoodsSubmitBtn",
					Targets:    []string{`[data-goods-id="1"]`, `[data-goods-id="3"]`, `[data-goods-id="4"]`, "#selectGoodsSubmitBtn"},
					ActionType: model.ActionSelectGoods,
					Validate:   validateGoodsList(1, 3, 4),
				},
				{
					ID:         "round5-set-starts",
					Title:      "设置本轮起点",
					Body:       "请把人参设为 5、丝绸设为 4、玉石设为 0，然后确认起点。",
					Target:     "#startsSubmitBtn",
					Targets:    []string{`.ship-start-input[data-ship-id="1"]`, `.ship-start-input[data-ship-id="3"]`, `.ship-start-input[data-ship-id="4"]`, "#startsSubmitBtn"},
					ActionType: model.ActionSetShipStarts,
					Validate:   validateStarts(map[string]int{"1": 5, "3": 4, "4": 0}),
					After: func(s *Service, g *model.Game) error {
						s.addTutorialNote(g, "第 5 轮准备", "本轮出航准备完成。轮到你放置同伙。")
						return nil
					},
				},
				{
					ID:         "round5-place-ginseng",
					Title:      "放置到人参船",
					Body:       "人参船起点靠前，先把同伙放到人参船；如果货船到港，船上同伙会先按船上收益结算，之后再处理货物升值。",
					Target:     `.ship[data-ship-id="1"]`,
					Targets:    []string{`.ship[data-ship-id="1"]`, tutorialPlaceButtonTarget("ship", 1)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 1),
					After: func(s *Service, g *model.Game) error {
						return scriptRound5ToSecondPlacement(s, g)
					},
				},
				{
					ID:         "round5-place-silk",
					Title:      "放置到丝绸船",
					Body:       "丝绸船当前位置也靠前，把同伙放到丝绸船。",
					Target:     `.ship[data-ship-id="3"]`,
					Targets:    []string{`.ship[data-ship-id="3"]`, tutorialPlaceButtonTarget("ship", 3)},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("ship", 3),
					After: func(s *Service, g *model.Game) error {
						return scriptRound5ToThirdPlacement(s, g)
					},
				},
				{
					ID:         "round5-place-port",
					Title:      "放置到港口 B",
					Body:       "已经有一艘船到港。把同伙放到港口 B；港口 B 需要至少两艘船到港才会结算。",
					Target:     `.port-overlay .board-slot[data-slot-id="B"]`,
					Targets:    []string{`.port-overlay .board-slot[data-slot-id="B"]`, tutorialPlaceButtonTarget("port", "B")},
					ActionType: model.ActionPlaceAccomplice,
					Validate:   validatePlace("port", "B"),
					After: func(s *Service, g *model.Game) error {
						return scriptRound5ToGameEnd(s, g)
					},
				},
				{
					ID:         "round5-settlement-break",
					Title:      "本轮结算",
					Body:       "第 5 轮结算已完成。你通过人参船获得 18 比索、丝绸船获得 30 比索、港口 B 获得 8 比索，本轮合计收益 56 比索。根据到港情况，人参股价上涨到 30，丝绸股价上涨到 20。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "game-end-trigger-break",
					Title:      "游戏结束判定",
					Body:       "游戏规定：当任意一种股票的股价到达 30 时，当前回合结算完成后游戏结束。此时人参股价已经到达 30，所以本局游戏结束，开始终局结算。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "final-stock-scoring-break",
					Title:      "终局资产结算",
					Body:       "最终得分 = 玩家现金 + 股票价值 - 抵押股票赎金。你的现金是 85；股票价值是人参 2 股 x 30 加丝绸 2 股 x 20，共 100；抵押股票赎金是 2 张 x 15，共 30。所以最终得分是 85 + 100 - 30 = 155。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
				{
					ID:         "tutorial-victory-break",
					Title:      "完成新手教学",
					Body:       "恭喜你完成新手教学，并在这局获得第一名。你已经掌握了完成一局所需的主要规则。",
					Target:     tutorialContinueButtonTarget(),
					Targets:    []string{tutorialContinueButtonTarget()},
					ActionType: model.ActionTutorialContinue,
					Validate:   validateType(model.ActionTutorialContinue),
				},
			}, nil
		},
	}
}

func tutorialChapterByID(id string) *tutorialChapter {
	chapters := tutorialChapters()
	if id == "" {
		return &chapters[0]
	}
	id = canonicalTutorialChapterID(id)
	for i := range chapters {
		if chapters[i].ID == id {
			return &chapters[i]
		}
	}
	return nil
}

func canonicalTutorialChapterID(id string) string {
	switch id {
	case "overview":
		return "overview"
	case "auction", "harbor":
		return "intro"
	case "placement", "special":
		return "placement_main"
	case "pirate_board", "pirate_loot", "navigator_big":
		return "voyage"
	case "settlement", "mortgage", "bankrupt":
		return "settlement_money"
	default:
		return id
	}
}

func tutorialChapterIndex(id string) int {
	id = canonicalTutorialChapterID(id)
	for i, chapter := range tutorialChapters() {
		if chapter.ID == id {
			return i
		}
	}
	return 0
}

func tutorialChapterSummaries() []model.TutorialSummary {
	chapters := tutorialChapters()
	out := make([]model.TutorialSummary, 0, len(chapters))
	for _, chapter := range chapters {
		out = append(out, model.TutorialSummary{ID: chapter.ID, Title: chapter.Title, Description: chapter.Description})
	}
	return out
}

func ensureStarted(s *Service, g *model.Game) error {
	if g.Status != model.StatusNotStarted {
		return nil
	}
	return s.engine.StartGame(g)
}

func scriptOverviewAuctionToPreparation(s *Service, g *model.Game) error {
	if err := ensureStarted(s, g); err != nil {
		return err
	}
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected overview auction phase, got %s", g.Phase)
	}
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 1, Type: model.ActionBid, Payload: map[string]interface{}{"amount": 8}},
		model.Action{PlayerID: 2, Type: model.ActionPassBid},
		model.Action{PlayerID: 3, Type: model.ActionPassBid},
		model.Action{PlayerID: 4, Type: model.ActionPassBid},
	); err != nil {
		return err
	}
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != 1 {
		return fmt.Errorf("expected P1 harbor master preparation, phase=%s harborMaster=%d", g.Phase, g.HarborMaster)
	}
	return nil
}

func scriptOverviewPreparationToPlacement(s *Service, g *model.Game) error {
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != 1 {
		return fmt.Errorf("expected overview preparation phase, phase=%s harborMaster=%d", g.Phase, g.HarborMaster)
	}
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 1, Type: model.ActionBuyShare, Payload: map[string]interface{}{"goodsId": 1}},
		model.Action{PlayerID: 1, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}},
		model.Action{PlayerID: 1, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 4, "2": 3, "3": 2}}},
	); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement {
		return fmt.Errorf("expected overview placement phase, got %s", g.Phase)
	}
	return nil
}

func scriptOverviewToFirstSailing(s *Service, g *model.Game) error {
	startMovement := g.Round.MovementStep
	return scriptOverviewUntil(s, g, 80, func(g *model.Game) bool {
		return g.Round.MovementStep > startMovement
	})
}

func scriptOverviewToRoundReview(s *Service, g *model.Game) error {
	return scriptOverviewUntil(s, g, 180, func(g *model.Game) bool {
		return g.Phase == model.PhaseRoundReview
	})
}

func scriptOverviewToMidgame(s *Service, g *model.Game) error {
	startRound := g.RoundNumber
	return scriptOverviewUntil(s, g, 600, func(g *model.Game) bool {
		return g.Phase == model.PhaseAuction && g.RoundNumber >= startRound+2
	})
}

func scriptOverviewToGameEnd(s *Service, g *model.Game) error {
	return scriptOverviewUntil(s, g, 2400, func(g *model.Game) bool {
		return g.Status == model.StatusEnded && g.Phase == model.PhaseGameEnd
	})
}

func scriptOverviewUntil(s *Service, g *model.Game, max int, done func(*model.Game) bool) error {
	for i := 0; i < max; i++ {
		if done(g) {
			return nil
		}
		current := roomActorForPhase(g)
		if g.Phase == model.PhaseRoundReview {
			current = g.CurrentPlayer
		}
		if current == 0 {
			return fmt.Errorf("no actor while overview scripting phase %s", g.Phase)
		}
		action, err := overviewScriptAction(s, g, current)
		if err != nil {
			return err
		}
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	return fmt.Errorf("overview script did not reach target from round %d phase %s", g.RoundNumber, g.Phase)
}

func overviewScriptAction(s *Service, g *model.Game, playerID int) (model.Action, error) {
	acts, err := s.engine.LegalActions(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	if len(acts) == 0 {
		return model.Action{}, fmt.Errorf("no overview script action for player %d", playerID)
	}
	for _, preferred := range []func(model.LegalAction) bool{
		func(a model.LegalAction) bool { return a.Type == model.ActionConfirmRound },
		func(a model.LegalAction) bool { return a.Type == model.ActionPirateSkipBoard },
		func(a model.LegalAction) bool {
			return a.Type == model.ActionNavigatorMove || a.Type == model.ActionNavigatorSkip
		},
		func(a model.LegalAction) bool { return a.Type == model.ActionPirateChooseDestination },
		func(a model.LegalAction) bool { return a.Type == model.ActionBuyShare },
		func(a model.LegalAction) bool { return a.Type == model.ActionSelectGoods },
		func(a model.LegalAction) bool { return a.Type == model.ActionSetShipStarts },
		func(a model.LegalAction) bool {
			return a.Type == model.ActionPlaceAccomplice && fmt.Sprint(a.Payload["positionType"]) == overviewPreferredPlacement(g, playerID)
		},
		func(a model.LegalAction) bool {
			return a.Type == model.ActionPlaceAccomplice && fmt.Sprint(a.Payload["positionType"]) == "ship"
		},
		func(a model.LegalAction) bool { return a.Type == model.ActionPlaceAccomplice },
		func(a model.LegalAction) bool { return a.Type == model.ActionSkipBuyShare },
		func(a model.LegalAction) bool { return a.Type == model.ActionPassBid },
	} {
		for _, act := range acts {
			if preferred(act) {
				payload := copyPayload(act.Payload)
				if act.Type == model.ActionSelectGoods {
					payload = map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}
				}
				if act.Type == model.ActionSetShipStarts {
					payload = map[string]interface{}{"starts": map[string]interface{}{"1": 4, "2": 3, "3": 2}}
				}
				if act.Type == model.ActionNavigatorMove {
					payload = map[string]interface{}{"moves": []interface{}{}}
				}
				if act.Type == model.ActionPirateChooseDestination {
					payload["destination"] = "port"
				}
				return model.Action{PlayerID: playerID, Type: act.Type, Payload: payload}, nil
			}
		}
	}
	act := acts[0]
	return model.Action{PlayerID: playerID, Type: act.Type, Payload: copyPayload(act.Payload)}, nil
}

func overviewPreferredPlacement(g *model.Game, playerID int) string {
	preferences := map[int]map[int]string{
		1: {1: "ship", 2: "port", 3: "dock", 4: "insurance"},
		2: {1: "ship", 2: "pirate", 3: "ship", 4: "port"},
		3: {1: "ship", 2: "dock", 3: "port", 4: "navigatorSmall"},
	}
	if byPlayer, ok := preferences[g.Round.PlacementStep]; ok {
		if position, ok := byPlayer[playerID]; ok {
			return position
		}
	}
	return "ship"
}

func setupPlayerPlacementRound(s *Service, g *model.Game, buyShare bool, starts map[string]int) error {
	if err := ensureStarted(s, g); err != nil {
		return err
	}
	return setupPlayerPlacementRoundFromAuction(s, g, buyShare, starts)
}

func setupPlayerPlacementRoundFromAuction(s *Service, g *model.Game, buyShare bool, starts map[string]int) error {
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected auction before placement setup, got %s", g.Phase)
	}
	if err := scriptPlayerWinsAuction(s, g, tutorialPlayerID, g.Auction.CurrentBid+1); err != nil {
		return err
	}
	return setupTutorialHarborMasterPreparation(s, g, buyShare, starts)
}

func setupTutorialHarborMasterPreparation(s *Service, g *model.Game, buyShare bool, starts map[string]int) error {
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != tutorialPlayerID {
		return fmt.Errorf("expected tutorial player harbor master buy-share phase, phase=%s harborMaster=%d", g.Phase, g.HarborMaster)
	}
	if buyShare {
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionBuyShare, Payload: map[string]interface{}{"goodsId": 1}}); err != nil {
			return err
		}
	} else if err := s.engine.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSkipBuyShare}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}); err != nil {
		return err
	}
	rawStarts := map[string]interface{}{}
	for key, value := range starts {
		rawStarts[key] = value
	}
	return s.engine.ApplyAction(g, model.Action{PlayerID: 1, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": rawStarts}})
}

func setupCashPoorPlacementRoundFromAuction(s *Service, g *model.Game, starts map[string]int) error {
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected auction before cash-poor placement setup, got %s", g.Phase)
	}
	bid := g.Players[tutorialPlayerID].Cash
	if bid <= g.Auction.CurrentBid {
		return fmt.Errorf("tutorial player cash %d cannot beat current bid %d", bid, g.Auction.CurrentBid)
	}
	if err := scriptPlayerWinsAuction(s, g, tutorialPlayerID, bid); err != nil {
		return err
	}
	return setupCashPoorPlacementAfterAuction(s, g, starts)
}

func setupCashPoorPlacementAfterAuction(s *Service, g *model.Game, starts map[string]int) error {
	if g.Phase == model.PhaseAuction {
		if err := finishTutorialAuctionAfterBid(s, g); err != nil {
			return err
		}
	}
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != tutorialPlayerID {
		return fmt.Errorf("expected tutorial player harbor master buy-share phase, phase=%s harborMaster=%d", g.Phase, g.HarborMaster)
	}
	if g.Players[tutorialPlayerID].Cash != 0 {
		return fmt.Errorf("expected tutorial player to spend all cash on harbor master, got %d", g.Players[tutorialPlayerID].Cash)
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: tutorialPlayerID, Type: model.ActionSkipBuyShare}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: tutorialPlayerID, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}); err != nil {
		return err
	}
	rawStarts := map[string]interface{}{}
	for key, value := range starts {
		rawStarts[key] = value
	}
	return s.engine.ApplyAction(g, model.Action{PlayerID: tutorialPlayerID, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": rawStarts}})
}

func scriptPlayerWinsAuction(s *Service, g *model.Game, playerID int, amount int) error {
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected auction, got %s", g.Phase)
	}
	if err := scriptPassUntilPlayer(s, g, playerID); err != nil {
		return err
	}
	if amount <= g.Auction.CurrentBid {
		amount = g.Auction.CurrentBid + 1
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: playerID, Type: model.ActionBid, Payload: map[string]interface{}{"amount": amount}}); err != nil {
		return err
	}
	for g.Phase == model.PhaseAuction {
		if g.CurrentPlayer == playerID {
			return fmt.Errorf("auction returned to tutorial player before ending")
		}
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionPassBid}); err != nil {
			return err
		}
	}
	return nil
}

func finishTutorialAuctionAfterBid(s *Service, g *model.Game) error {
	if g.Phase == model.PhaseHarborMasterBuyShare && g.HarborMaster == tutorialPlayerID {
		return nil
	}
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected auction after tutorial bid, got %s", g.Phase)
	}
	if !g.Auction.HasAnyBid || g.Auction.HighestBidder != tutorialPlayerID {
		return fmt.Errorf("expected tutorial player to be highest bidder, highest=%d currentBid=%d", g.Auction.HighestBidder, g.Auction.CurrentBid)
	}
	for g.Phase == model.PhaseAuction {
		if g.CurrentPlayer == tutorialPlayerID {
			return fmt.Errorf("auction returned to tutorial player before ending")
		}
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionPassBid}); err != nil {
			return err
		}
	}
	if g.Phase != model.PhaseHarborMasterBuyShare || g.HarborMaster != tutorialPlayerID {
		return fmt.Errorf("expected tutorial player harbor master after auction, phase=%s harborMaster=%d", g.Phase, g.HarborMaster)
	}
	return nil
}

func scriptPassUntilPlayer(s *Service, g *model.Game, playerID int) error {
	for g.Phase == model.PhaseAuction && g.CurrentPlayer != playerID {
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionPassBid}); err != nil {
			return err
		}
	}
	return nil
}

func scriptToNextAuction(s *Service, g *model.Game) error {
	startRound := g.RoundNumber
	return scriptUntil(s, g, 220, func(g *model.Game) bool {
		return g.Phase == model.PhaseAuction && g.RoundNumber > startRound
	})
}

func scriptUntilPlacementTurn(s *Service, g *model.Game, placementStep int) error {
	return scriptUntil(s, g, 80, func(g *model.Game) bool {
		return g.Phase == model.PhasePlacement &&
			g.CurrentPlayer == tutorialPlayerID &&
			g.Round.PlacementStep == placementStep &&
			g.Round.PlacementTurnsTaken == 0
	})
}

func scriptUntilPhaseForPlayer(s *Service, g *model.Game, phase model.Phase) error {
	return scriptUntil(s, g, 100, func(g *model.Game) bool {
		return g.Phase == phase && g.CurrentPlayer == tutorialPlayerID
	})
}

func scriptUntil(s *Service, g *model.Game, max int, done func(*model.Game) bool) error {
	for i := 0; i < max; i++ {
		if done(g) {
			return nil
		}
		current := roomActorForPhase(g)
		if g.Phase == model.PhaseRoundReview {
			current = g.CurrentPlayer
		}
		if current == 0 {
			return fmt.Errorf("no actor while scripting phase %s", g.Phase)
		}
		action, err := tutorialScriptAction(s, g, current)
		if err != nil {
			return err
		}
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	return fmt.Errorf("tutorial script did not reach target from phase %s", g.Phase)
}

func setupRoundReviewByScript(s *Service, g *model.Game) error {
	if err := setupPlayerPlacementRound(s, g, true, map[string]int{"1": 5, "2": 4, "3": 0}); err != nil {
		return err
	}
	for _, action := range []model.Action{
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "C"}},
	} {
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	if g.Phase != model.PhaseRoundReview || g.CurrentPlayer != tutorialPlayerID {
		return fmt.Errorf("expected round review after settlement setup, got phase %s player %d", g.Phase, g.CurrentPlayer)
	}
	return nil
}

func setupSettlementPlacementStart(s *Service, g *model.Game) error {
	if err := setupPlayerPlacementRound(s, g, false, map[string]int{"1": 5, "2": 2, "3": 2}); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.Round.PlacementStep != 1 {
		return fmt.Errorf("expected first settlement placement turn, got phase %s player %d step %d", g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func setupSettlementPlacementAfterAuction(s *Service, g *model.Game) error {
	if err := finishTutorialAuctionAfterBid(s, g); err != nil {
		return err
	}
	if err := setupTutorialHarborMasterPreparation(s, g, false, map[string]int{"1": 5, "2": 2, "3": 2}); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 1 || g.Round.PlacementStep != 1 {
		return fmt.Errorf("expected first settlement placement turn, got round %d phase %s player %d step %d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func setupSettlementLootingByScript(s *Service, g *model.Game) error {
	if err := setupPlayerPlacementRound(s, g, true, map[string]int{"1": 5, "2": 2, "3": 2}); err != nil {
		return err
	}
	for _, action := range []model.Action{
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 2}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}},
		{PlayerID: 1, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 3}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
	} {
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	if g.Phase != model.PhasePirateLooting || g.CurrentPlayer != tutorialPlayerID {
		return fmt.Errorf("expected pirate looting after settlement setup, got phase %s player %d", g.Phase, g.CurrentPlayer)
	}
	if len(g.Round.PendingLootShips) != 2 ||
		g.Round.PendingLootShips[0] != model.GoodsNutmeg ||
		g.Round.PendingLootShips[1] != model.GoodsSilk {
		return fmt.Errorf("expected nutmeg and silk pending pirate loot, got %+v", g.Round.PendingLootShips)
	}
	return nil
}

func setupBankruptPlacementTurn(s *Service, g *model.Game) error {
	if g.Phase != model.PhaseAuction {
		return fmt.Errorf("expected auction before bankrupt setup, got %s", g.Phase)
	}
	if err := scriptPlayerWinsAuction(s, g, 2, g.Auction.CurrentBid+1); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSkipBuyShare}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 3, "2": 3, "3": 3}}}); err != nil {
		return err
	}
	for _, action := range []model.Action{
		{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
	} {
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	g.Players[1].Cash = 0
	for _, gid := range model.GoodsOrder {
		g.Players[1].Shares[gid] = 0
	}
	g.Players[1].MortgagedShareCount = 0
	return nil
}

func setupBankruptPlacementAfterAuctionPass(s *Service, g *model.Game) error {
	if g.Phase != model.PhaseAuction || g.CurrentPlayer != 2 {
		return fmt.Errorf("expected auction after tutorial player pass, got phase %s player %d", g.Phase, g.CurrentPlayer)
	}
	if err := scriptPlayerWinsAuction(s, g, 2, g.Auction.CurrentBid+1); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSkipBuyShare}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSelectGoods, Payload: map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}}); err != nil {
		return err
	}
	if err := s.engine.ApplyAction(g, model.Action{PlayerID: 2, Type: model.ActionSetShipStarts, Payload: map[string]interface{}{"starts": map[string]interface{}{"1": 1, "2": 3, "3": 5}}}); err != nil {
		return err
	}
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
	); err != nil {
		return err
	}
	if _, err := s.engine.LegalActions(g, tutorialPlayerID); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || !g.Players[tutorialPlayerID].BankruptThisRound {
		return fmt.Errorf("expected bankrupt placement for tutorial player, phase=%s player=%d bankrupt=%v", g.Phase, g.CurrentPlayer, g.Players[tutorialPlayerID].BankruptThisRound)
	}
	return nil
}

func scriptBankruptStowawayToSecondPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 1}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
	); err != nil {
		return err
	}
	return assertBankruptStowawayTarget(s, g, model.GoodsNutmeg)
}

func scriptBankruptStowawayToThirdPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "navigatorBig", "targetId": "big"}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "C"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
	); err != nil {
		return err
	}
	return assertBankruptStowawayTarget(s, g, model.GoodsSilk)
}

func scriptBankruptStowawayRoundToReview(s *Service, g *model.Game) error {
	if g.Phase == model.PhaseNavigatorAction {
		if err := s.engine.ApplyAction(g, model.Action{PlayerID: g.CurrentPlayer, Type: model.ActionNavigatorMove, Payload: map[string]interface{}{"moves": []interface{}{}}}); err != nil {
			return err
		}
	}
	return scriptUntil(s, g, 120, func(g *model.Game) bool {
		return g.Phase == model.PhaseRoundReview
	})
}

func assertBankruptStowawayTarget(s *Service, g *model.Game, shipID model.GoodsID) error {
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || !g.Players[tutorialPlayerID].BankruptThisRound {
		return fmt.Errorf("expected bankrupt stowaway placement for tutorial player, phase=%s player=%d bankrupt=%v", g.Phase, g.CurrentPlayer, g.Players[tutorialPlayerID].BankruptThisRound)
	}
	acts, err := s.engine.LegalActions(g, tutorialPlayerID)
	if err != nil {
		return err
	}
	for _, act := range acts {
		if act.Type == model.ActionPlaceAccomplice &&
			act.Payload["isStowaway"] == true &&
			fmt.Sprint(act.Payload["targetId"]) == fmt.Sprint(int(shipID)) {
			return nil
		}
	}
	return fmt.Errorf("expected stowaway target %d, got %+v", shipID, acts)
}

func setupRound4ProfitAfterAuction(s *Service, g *model.Game) error {
	if err := finishTutorialAuctionAfterBid(s, g); err != nil {
		return err
	}
	if err := setupTutorialHarborMasterPreparation(s, g, true, map[string]int{"1": 4, "2": 0, "3": 5}); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 4 || g.Round.PlacementStep != 1 {
		return fmt.Errorf("expected round 4 first placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	s.addTutorialNote(g, "第 4 轮准备", "出航准备已完成。你买入 1 张人参股份，轮到你放置同伙。")
	return nil
}

func scriptRound4ToSecondPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
	); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 4 || g.Round.PlacementStep != 2 {
		return fmt.Errorf("expected round 4 second placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func scriptRound4ToThirdPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "B"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "C"}},
	); err != nil {
		return err
	}
	if g.Ships[model.GoodsSilk].Status != model.ShipArrived {
		return fmt.Errorf("expected silk to arrive before round 4 third placement, got %+v", g.Ships[model.GoodsSilk])
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 4 || g.Round.PlacementStep != 3 {
		return fmt.Errorf("expected round 4 third placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func scriptRound4ToLooting(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "C"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 2}},
	); err != nil {
		return err
	}
	if g.Phase != model.PhasePirateLooting || g.CurrentPlayer != tutorialPlayerID ||
		len(g.Round.PendingLootShips) != 1 || g.Round.PendingLootShips[0] != model.GoodsGinseng {
		return fmt.Errorf("expected round 4 ginseng pirate looting, phase=%s player=%d pending=%+v", g.Phase, g.CurrentPlayer, g.Round.PendingLootShips)
	}
	return nil
}

func setupRound5FinalAfterAuction(s *Service, g *model.Game) error {
	if err := finishTutorialAuctionAfterBid(s, g); err != nil {
		return err
	}
	if err := setupTutorialHarborMasterPreparation(s, g, true, map[string]int{"1": 5, "3": 4, "4": 0}); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 5 || g.Round.PlacementStep != 1 {
		return fmt.Errorf("expected round 5 first placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	s.addTutorialNote(g, "第 5 轮准备", "本轮出航准备已完成，轮到你放置同伙。")
	return nil
}

func scriptRound5ToSecondPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "A"}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "B"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "dock", "targetId": "C"}},
	); err != nil {
		return err
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 5 || g.Round.PlacementStep != 2 {
		return fmt.Errorf("expected round 5 second placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func scriptRound5ToThirdPlacement(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "A"}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 4}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "insurance", "targetId": "insurance"}},
	); err != nil {
		return err
	}
	if g.Ships[model.GoodsSilk].Status != model.ShipArrived {
		return fmt.Errorf("expected silk to arrive before round 5 third placement, got %+v", g.Ships[model.GoodsSilk])
	}
	if g.Phase != model.PhasePlacement || g.CurrentPlayer != tutorialPlayerID || g.RoundNumber != 5 || g.Round.PlacementStep != 3 {
		return fmt.Errorf("expected round 5 third placement, round=%d phase=%s player=%d step=%d", g.RoundNumber, g.Phase, g.CurrentPlayer, g.Round.PlacementStep)
	}
	return nil
}

func scriptRound5ToGameEnd(s *Service, g *model.Game) error {
	if err := applyScriptActions(s, g,
		model.Action{PlayerID: 2, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "ship", "targetId": 4}},
		model.Action{PlayerID: 3, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "port", "targetId": "C"}},
		model.Action{PlayerID: 4, Type: model.ActionPlaceAccomplice, Payload: map[string]interface{}{"positionType": "pirate", "targetId": 1}},
	); err != nil {
		return err
	}
	if g.Status != model.StatusEnded || g.Phase != model.PhaseGameEnd {
		return fmt.Errorf("expected game end after round 5, status=%s phase=%s", g.Status, g.Phase)
	}
	if g.Goods[model.GoodsGinseng].MarketValue() != 30 {
		return fmt.Errorf("expected ginseng to reach 30, got %d", g.Goods[model.GoodsGinseng].MarketValue())
	}
	return nil
}

func tutorialScriptAction(s *Service, g *model.Game, playerID int) (model.Action, error) {
	acts, err := s.engine.LegalActions(g, playerID)
	if err != nil {
		return model.Action{}, err
	}
	if len(acts) == 0 {
		return model.Action{}, fmt.Errorf("no script action for player %d", playerID)
	}
	for _, preferred := range []func(model.LegalAction) bool{
		func(a model.LegalAction) bool { return a.Type == model.ActionConfirmRound },
		func(a model.LegalAction) bool { return a.Type == model.ActionPirateSkipBoard },
		func(a model.LegalAction) bool { return a.Type == model.ActionNavigatorMove },
		func(a model.LegalAction) bool { return a.Type == model.ActionSelectGoods },
		func(a model.LegalAction) bool { return a.Type == model.ActionSetShipStarts },
		func(a model.LegalAction) bool {
			return a.Type == model.ActionPlaceAccomplice && fmt.Sprint(a.Payload["positionType"]) == "ship"
		},
		func(a model.LegalAction) bool { return a.Type == model.ActionPlaceAccomplice },
		func(a model.LegalAction) bool { return a.Type == model.ActionSkipBuyShare },
		func(a model.LegalAction) bool { return a.Type == model.ActionPassBid },
	} {
		for _, act := range acts {
			if preferred(act) {
				payload := copyPayload(act.Payload)
				if act.Type == model.ActionBid {
					minBid, _ := payloadInt(payload["minBid"])
					payload["amount"] = minBid
				}
				if act.Type == model.ActionSelectGoods {
					payload = map[string]interface{}{"goodsIds": []interface{}{1, 2, 3}}
				}
				if act.Type == model.ActionSetShipStarts {
					payload = map[string]interface{}{"starts": map[string]interface{}{"1": 3, "2": 3, "3": 3}}
				}
				if act.Type == model.ActionNavigatorMove {
					payload = map[string]interface{}{"moves": []interface{}{}}
				}
				return model.Action{PlayerID: playerID, Type: act.Type, Payload: payload}, nil
			}
		}
	}
	act := acts[0]
	return model.Action{PlayerID: playerID, Type: act.Type, Payload: copyPayload(act.Payload)}, nil
}

func applyScriptActions(s *Service, g *model.Game, actions ...model.Action) error {
	for _, action := range actions {
		if action.Payload == nil {
			action.Payload = map[string]interface{}{}
		}
		if err := s.engine.ApplyAction(g, action); err != nil {
			return err
		}
	}
	return nil
}

func validateType(actionType model.ActionType) func(model.Action) bool {
	return func(action model.Action) bool {
		return action.Type == actionType
	}
}

func validateBid(amount int) func(model.Action) bool {
	return func(action model.Action) bool {
		got, ok := payloadInt(action.Payload["amount"])
		return action.Type == model.ActionBid && ok && got == amount
	}
}

func validatePayload(key string, expected interface{}) func(model.Action) bool {
	return func(action model.Action) bool {
		return fmt.Sprint(action.Payload[key]) == fmt.Sprint(expected)
	}
}

func validatePlace(positionType string, target interface{}) func(model.Action) bool {
	return func(action model.Action) bool {
		if action.Type != model.ActionPlaceAccomplice {
			return false
		}
		return fmt.Sprint(action.Payload["positionType"]) == positionType && fmt.Sprint(action.Payload["targetId"]) == fmt.Sprint(target)
	}
}

func validateGoodsList(expected ...int) func(model.Action) bool {
	sort.Ints(expected)
	return func(action model.Action) bool {
		raw, ok := action.Payload["goodsIds"].([]interface{})
		if !ok || len(raw) != len(expected) {
			return false
		}
		got := make([]int, 0, len(raw))
		for _, item := range raw {
			n, ok := payloadInt(item)
			if !ok {
				return false
			}
			got = append(got, n)
		}
		sort.Ints(got)
		for i := range expected {
			if got[i] != expected[i] {
				return false
			}
		}
		return true
	}
}

func validateStarts(expected map[string]int) func(model.Action) bool {
	return func(action model.Action) bool {
		raw, ok := action.Payload["starts"].(map[string]interface{})
		if !ok || len(raw) != len(expected) {
			return false
		}
		for key, want := range expected {
			got, ok := payloadInt(raw[key])
			if !ok || got != want {
				return false
			}
		}
		return true
	}
}

func validateMoves(expected map[int]int) func(model.Action) bool {
	return func(action model.Action) bool {
		raw, ok := action.Payload["moves"].([]interface{})
		if !ok {
			return false
		}
		got := map[int]int{}
		for _, item := range raw {
			move, ok := item.(map[string]interface{})
			if !ok {
				return false
			}
			shipID, ok := payloadInt(move["shipId"])
			if !ok {
				return false
			}
			delta, ok := payloadInt(move["delta"])
			if !ok {
				return false
			}
			if delta != 0 {
				got[shipID] = delta
			}
		}
		if len(got) != len(expected) {
			return false
		}
		for shipID, want := range expected {
			if got[shipID] != want {
				return false
			}
		}
		return true
	}
}

func validateDestination(shipID int, destination string) func(model.Action) bool {
	return func(action model.Action) bool {
		gotShip, ok := payloadInt(action.Payload["shipId"])
		return ok && gotShip == shipID && fmt.Sprint(action.Payload["destination"]) == destination
	}
}
