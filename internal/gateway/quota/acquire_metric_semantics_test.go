package quota

// 本包内 fail-open 计数器的**观测归属**说明（避免后续有人重复造用例而互相打断）：
//
// metrics.go 的计数器是包级懒初始化（metricsOnce，全进程只能绑定一次 meter provider）。
// 因此同一进程内**只能有一个**用例有效地观测 gateway_quota_fail_open_total；先跑到的那个
// 会用自己安装的 provider 完成初始化，后跑的观测会读到空结果并只能 Skip。
//
// 本包该断言的**权威用例**是 verifier 编写的：
//   TestVerifierT3_OverLimitIsNotFailOpen_BusinessVsInfra（lease_semantics_verify_test.go）
// 它断言两点：业务超限（并发 + RPM）不得增加 fail-open 计数；真实 Redis 故障计数必须为 1。
// 隔离执行以获得有效断言：
//   go test ./internal/gateway/quota/ -run TestVerifierT3_OverLimitIsNotFailOpen -count=1 -v
//
// 我（engineer-quota）此前写过一份同类观测用例，实测它会凭文件名排序抢先占用 metricsOnce，
// 导致上面这个权威用例退化成交付期 Skip —— 因此已删除，把观测权让给 verifier 的用例。
//
// 不依赖计量装置的等价证据（顺序无关，始终执行）：
//   - TestAcquireConcurrency_NilReply_RejectsInsteadOfFailOpen / TestCheckRPM_NilReply_...：
//     在客户端 hook 层注入 redis.Nil 空回复，断言必须**拒绝**而不是 fail-open 放行；
//   - TestAcquireConcurrency_LimitPlusOne_ReturnsNonNilErrorAndNoResidue：断言第 limit+1 次
//     返回**非 nil error**、错误为 concurrency_exceeded（不是 protocol_error）、ZCARD 不增长；
//   - TestAcquireConcurrency_SustainedOverLimit_DoesNotGrowZCARD：连续 10 次被拒后 ZCARD 不变。
