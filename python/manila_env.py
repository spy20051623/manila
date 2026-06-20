from __future__ import annotations

# Reserved for future Python AI experiments.
#
# The current Manila backend, web UI, and built-in AI training all run in Go.
# This lightweight wrapper is kept only as a future integration point for
# Python-based AI code that wants to drive the Go backend through HTTP.

import requests


class ManilaEnv:
    """Small Python wrapper for the Go Manila backend.

    This is intentionally lightweight: the Go server remains the authority for
    rules, legal actions, random seeds, and scoring.
    """

    def __init__(self, base_url: str = "http://localhost:8080"):
        self.base_url = base_url.rstrip("/")
        self.game_id: str | None = None
        self.state: dict | None = None

    def reset(self, seed: int | None = None) -> dict:
        payload = {} if seed is None else {"seed": seed}
        created = self._post("/games", payload)
        self.game_id = created["game"]["gameId"]
        started = self._post(f"/games/{self.game_id}/start", {})
        self.state = started["game"]
        return self.state

    def legal_actions(self, player_id: int | None = None) -> list[dict]:
        self._require_game()
        if player_id is None:
            player_id = int(self.state["currentPlayer"])
        data = self._get(f"/games/{self.game_id}/players/{player_id}/actions")
        return data["actions"]

    def step(self, action: dict) -> tuple[dict, bool, list[dict]]:
        self._require_game()
        if "playerId" not in action:
            action["playerId"] = int(self.state["currentPlayer"])
        action.setdefault("expectedEventSeq", self.state["eventSeq"])
        data = self._post(f"/games/{self.game_id}/actions", action)
        self.state = data["game"]
        done = self.state["status"] == "ended"
        return self.state, done, self.state.get("finalScores", [])

    def random_ai_step(self, player_id: int | None = None) -> tuple[dict, bool, dict]:
        self._require_game()
        if player_id is None:
            player_id = int(self.state["currentPlayer"])
        data = self._post(f"/games/{self.game_id}/players/{player_id}/random-ai", {})
        self.state = data["game"]
        return self.state, self.state["status"] == "ended", data["action"]

    def _get(self, path: str) -> dict:
        response = requests.get(self.base_url + path, timeout=30)
        response.raise_for_status()
        return response.json()

    def _post(self, path: str, payload: dict) -> dict:
        response = requests.post(self.base_url + path, json=payload, timeout=30)
        response.raise_for_status()
        return response.json()

    def _require_game(self) -> None:
        if not self.game_id or self.state is None:
            raise RuntimeError("call reset() first")


if __name__ == "__main__":
    env = ManilaEnv()
    state = env.reset(seed=1)
    for _ in range(20):
        state, done, action = env.random_ai_step()
        print(state["phase"], state["currentPlayer"], action["type"])
        if done:
            print(state["finalScores"])
            break
