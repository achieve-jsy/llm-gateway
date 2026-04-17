import http from "k6/http";
import { check } from "k6";
import { Trend } from "k6/metrics";

const budgetCheck = new Trend("budget_check_ms", true);
const baseURL = __ENV.BASE_URL;
const apiKey = __ENV.API_KEY;
const model = __ENV.MODEL;

if (!baseURL || !apiKey || !model) {
  throw new Error("BASE_URL, API_KEY and MODEL are required");
}

export const options = {
  scenarios: {
    warmup: {
      executor: "constant-arrival-rate",
      rate: 100,
      timeUnit: "1s",
      duration: "2m",
      preAllocatedVUs: 50,
      maxVUs: 300,
      gracefulStop: "30s",
    },
    tier_budget: {
      executor: "constant-arrival-rate",
      startTime: "2m",
      rate: 1000,
      timeUnit: "1s",
      duration: "5m",
      preAllocatedVUs: 300,
      maxVUs: 1500,
      gracefulStop: "30s",
    },
    cooldown: {
      executor: "constant-arrival-rate",
      startTime: "7m",
      rate: 100,
      timeUnit: "1s",
      duration: "2m",
      preAllocatedVUs: 50,
      maxVUs: 300,
      gracefulStop: "30s",
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(99)<2000"],
    budget_check_ms: ["p(99)<2"],
  },
};

export default function () {
  const started = Date.now();
  const response = http.post(
    `${baseURL}/v1/chat/completions`,
    JSON.stringify({
      model,
      messages: [{ role: "user", content: "Reply with OK." }],
      max_tokens: 2,
      stream: false,
    }),
    {
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      tags: { feature: "model-tier-budget" },
    },
  );
  budgetCheck.add(Number(response.headers["X-Budget-Check-Ms"] || Date.now() - started));
  check(response, {
    "request accepted or budget blocked": (r) => r.status === 200 || r.status === 402,
    "402 has stable code": (r) =>
      r.status !== 402 ||
      ["budget_exceeded", "tier_budget_exceeded"].includes(r.json("error.code")),
  });
}
