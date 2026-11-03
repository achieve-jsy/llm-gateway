/* ═══════════════ Data（数据契约 = 真实端点 schema）═══════════════
   数据提供者抽象：原型用 MockProvider，接真实后端时换成 ApiProvider。
   两个方法对齐真实端点：
   modelHourly(params)  -> { Hourly:[{hour,group_key,prompt_tokens,cached_tokens,completion_tokens,est_cost_usd,call_count}],
                             Daily:[{date,group_key,...}] }   // = /usage/dashboard/model-hourly
   modelUsers(params)   -> { Depts:[{dept_name,user_count,cost_per_user,cost_pct}],
                             UserPerDay:[...], Monthly:[...] }  // = /usage/dashboard/model-users
*/
const MODELS = [
  { id:'gpt-4o',          tier:'L3-Premium',  in:0.000012, out:0.000060, cache:0.0000012 },
  { id:'claude-sonnet-5', tier:'L2-Advanced', in:0.000003, out:0.000015, cache:0.0000003 },
  { id:'deepseek-v3',     tier:'L1-Basic',    in:0.000004, out:0.000016, cache:0.0000004 },
  { id:'llama-3.1-8b',    tier:'unclassified',in:0.000002, out:0.000008, cache:0.0000002 },
];
const TIER_LABEL = { 'L3-Premium':'L3 · 高级', 'L2-Advanced':'L2 · 进阶', 'L1-Basic':'L1 · 基础', 'unclassified':'未分级' };
const HIGH_TIERS = new Set(['L3-Premium']);
const TIER_HEX = { 'L3-Premium':'#f97316', 'L2-Advanced':'#8b5cf6', 'L1-Basic':'#06b6d4', 'unclassified':'#6b6b75' };
const DEPTS = ['AI研发中心','数据平台部','产品设计部','运营管理部','X-Lab','(未分配)'];
const USERS = {
  'AI研发中心':['张伟','李娜','王磊','刘洋'], '数据平台部':['陈静','赵鑫','吴雨'],
  '产品设计部':['周涛','徐明','孙颖'], '运营管理部':['黄辉','马丽'],
  'X-Lab':['林峰','曹宇','谢婷'], '(未分配)':['服务账号'],
};
/* 当前区间 3 天 + 上一区间 3 天 → 环比可算（前端双请求相邻区间） */
const CURR_DAYS = ['2026-07-28','2026-07-29','2026-07-30'];
const PREV_DAYS = ['2026-07-25','2026-07-26','2026-07-27'];
const TOKEN_HEX = { prompt_tokens:'#3b82f6', cached_tokens:'#f59e0b', completion_tokens:'#10b981' };
const TOOLTIP_STYLE = { backgroundColor:'#1f2937', borderColor:'#374151', borderWidth:1, titleColor:'#e5e7eb', bodyColor:'#9ca3af', padding:10, cornerRadius:8, boxWidth:8, boxHeight:8 };
let isDark = false;
const HEAT_LIGHT=['#F1F5F9','#DBEAFE','#93C5FD','#3B82F6','#F59E0B'], HEAT_DARK=['#1E293B','#1E3A8A','#1D4ED8','#3B82F6','#F59E0B'];
const ACT  = { 'AI研发中心':1.6, '数据平台部':1.2, '产品设计部':0.9, '运营管理部':0.7, 'X-Lab':1.4, '(未分配)':0.1 };
/* 上一区间整体下调：让环比有涨有跌、可讲 */
const PREV_ACT = { 'AI研发中心':1.15, '数据平台部':0.95, '产品设计部':0.78, '运营管理部':0.68, 'X-Lab':1.05, '(未分配)':0.1 };
const PREF = {
  'AI研发中心':{'gpt-4o':0.40,'claude-sonnet-5':0.22,'deepseek-v3':0.34,'llama-3.1-8b':0.04},
  '数据平台部':{'gpt-4o':0.28,'claude-sonnet-5':0.28,'deepseek-v3':0.40,'llama-3.1-8b':0.04},
  '产品设计部':{'gpt-4o':0.18,'claude-sonnet-5':0.28,'deepseek-v3':0.50,'llama-3.1-8b':0.04},
  '运营管理部':{'gpt-4o':0.10,'claude-sonnet-5':0.26,'deepseek-v3':0.58,'llama-3.1-8b':0.06},
  'X-Lab':{'gpt-4o':0.40,'claude-sonnet-5':0.16,'deepseek-v3':0.40,'llama-3.1-8b':0.04},
  '(未分配)':{'gpt-4o':0.10,'claude-sonnet-5':0.10,'deepseek-v3':0.40,'llama-3.1-8b':0.40},
};
/* 力量用户倍率：让某部门人均费用显著超全司中位数，触发"人均异常"告警（可下钻到人） */
const USER_K = { '曹宇': 3.0 };
const HOUR_W = [0.3,0.2,0.15,0.1,0.1,0.2,0.5,1.0,1.6,1.9,1.8,1.7,1.4,1.5,1.8,1.9,1.7,1.4,1.1,0.8,0.6,0.5,0.4,0.35];

function h2(s){ let h=2166136261; for(let i=0;i<s.length;i++){h^=s.charCodeAt(i);h=Math.imul(h,16777619);} return h>>>0; }
function det(s,lo,hi){ const h=h2(s); return lo+(h%(hi-lo+1)); }
const isHigh = r => HIGH_TIERS.has(r.tier);
const tierOf = k => { if(TIER_LABEL[k]) return k; const m=MODELS.find(x=>x.id===k); return m?m.tier:'unclassified'; };
const monthOf = d => d.slice(0,7);
const hhLab = (date,h)=>date.slice(5)+' '+String(h).padStart(2,'0')+':00';

/* ═══════════════ Mock Provider ═══════════════ */
const ALL_ROWS = [];   // 扁平宽表（真实是预聚合，前端按需派生，模拟 ApiProvider 输出）
(function(){
  for(const [days,act] of [[PREV_DAYS,true],[CURR_DAYS,false]]){
    const ACT_NOW = act?PREV_ACT:ACT, tag = act?'prev':'curr';
    for(const dept of DEPTS) for(const user of USERS[dept]) for(const date of days) for(let hh=0;hh<24;hh++){
      const w=HOUR_W[hh]*ACT_NOW[dept]*(USER_K[user]||1);
      for(const m of MODELS){
        const calls=Math.round(det(user+tag+date+hh+m.id+'c',0,6)*PREF[dept][m.id]);
        if(!calls) continue;
        const base=56000*w;
        const prompt=Math.round(base*det(user+tag+date+hh+m.id+'p',70,130)/100);
        const cached=Math.round(prompt*det(user+tag+date+hh+m.id+'k',0,55)/100);
        const completion=Math.round(prompt*det(user+tag+date+hh+m.id+'o',28,80)/100);
        const cost=prompt*m.in+cached*m.cache+completion*m.out;
        ALL_ROWS.push({model:m.id,tier:m.tier,date,hour:hh,period:tag,user_id:'u-'+h2(user).toString(36),user_name:user,dept_name:dept,prompt_tokens:prompt,cached_tokens:cached,completion_tokens:completion,call_count:calls,est_cost_usd:Math.round(cost*10000)/10000});
      }
    }
  }
})();
/* ApiProvider 契约（真实后端）
async function modelHourly({start,end,model_name,tier_name,group_by}){
  const rows=ALL_ROWS.filter(r=>r.date>=start&&r.date<=end&&(!model_name||r.model===model_name)&&(!tier_name||r.tier===tier_name));
  const by=group_by==='tier'?r=>r.tier:r=>r.model, Hourly=[],Daily={};
  for(const r of rows){const k=by(r);Hourly.push({hour:hhLab(r.date,r.hour),group_key:k,prompt_tokens:r.prompt_tokens,cached_tokens:r.cached_tokens,completion_tokens:r.completion_tokens,est_cost_usd:r.est_cost_usd,call_count:r.call_count});}
  return {Hourly,Daily:[]};
}
*/

/* ═══════════════ State ═══════════════ */
let state={models:new Set(),tiers:new Set(),depts:new Set()},period='day',userShowAll=false,openModel=null,openDept=null,detailDay='all',defaultedModel=false;
const BUDGET_PER_DAY=1400;   // mock 预算：每日额度 → 区间预算 = 3 天
const fmtN=n=>n>=1e6?(n/1e6).toFixed(2)+'M':n>=1e3?(n/1e3).toFixed(1)+'K':String(Math.round(n));
const fmtC=c=>'$'+Number(c).toFixed(2);
function filtered(rows,periodTag){let src=rows||ALL_ROWS;if(!periodTag)src=src.filter(r=>r.period==='curr');else if(periodTag!=='all')src=src.filter(r=>r.period===periodTag);return src.filter(r=>(!state.models.size||state.models.has(r.model))&&(!state.tiers.size||state.tiers.has(r.tier))&&(!state.depts.size||state.depts.has(r.dept_name)));}
function by(fn,rows){const m={};for(const r of rows){const k=fn(r);(m[k]=m[k]||[]).push(r);}return m;}
function sum(rows,f){return rows.reduce((s,r)=>s+f(r),0);}
function usersOf(rows){const s=new Set();for(const r of rows)s.add(r.user_id);return s.size;}
function pctStr(v,total){return total>0?(v/total*100).toFixed(1)+'%':'0%';}
function deltaPct(a,b){return b>0?(a-b)/b*100:0;}
function median(arr){if(!arr.length)return 0;const s=[...arr].sort((x,y)=>x-y),m=Math.floor(s.length/2);return s.length%2?s[m]:(s[m-1]+s[m])/2;}

/* ═══════════════ Chart Registry ═══════════════ */
let trendChart=null,detailStackChart=null,detailHeatChart=null,detailTokChart=null;
function destroyCharts(...list){list.filter(Boolean).forEach(c=>c.destroy());}
const gridCol=()=>isDark?'rgba(148,163,184,.12)':'rgba(100,116,139,.12)';
const axCol=()=>isDark?'#94A3B8':'#64748B';
function baseLegend(){return {labels:{boxWidth:10,font:{size:11},color:isDark?'#CBD5E1':'#64748B',padding:14}};}

/* ═══════════════ Verdict（裁决条 · 红绿灯）═══════════════ */
function renderVerdict(cost,prevCost,hp,budget){
  const el=document.getElementById('verdict');
  const d=deltaPct(cost,prevCost),absD=Math.abs(d);
  const over=budget>0&&cost>budget;
  const hiHp=hp>=65,hiDelta=d>12;
  let st,cls,line2;
  if(over){cls='bad';st='超支';line2=`区间费用 <b class="cost">${fmtC(cost)}</b> 已超预算 <b>${fmtC(budget)}</b>；高阶模型占比 <b>${hp.toFixed(1)}%</b>。`;}
  else if(hiHp&&hiDelta){cls='bad';st='需干预';line2=`费用环比 <b class="t-red">+${absD.toFixed(1)}%</b> 且高阶占比 <b class="t-red">${hp.toFixed(1)}%</b>，重点排查高阶模型用量与人均异常部门。`;}
  else if(hiDelta||hiHp){cls='warn';st='需关注';line2=`费用环比 <b class="t-${hiDelta?'red':'amber'}">${d>0?'+':''}${d.toFixed(1)}%</b>${hiHp?`，高阶模型占比 <b class="t-amber">${hp.toFixed(1)}%</b>`:''}，建议关注下列异常清单。`;}
  else{cls='ok';st='受控';line2=`费用环比 <b class="t-green">${d>0?'+':''}${d.toFixed(1)}%</b>，高阶占比 <b>${hp.toFixed(1)}%</b> 受控，无需干预。`;}
  el.className='verdict '+cls;
  el.innerHTML=`<span class="v-dot"></span><div class="v-body"><div class="v-line1"><span class="status">${st}</span>总费用 <b>${fmtC(cost)}</b></div><div class="v-line2">${line2}</div></div><a class="v-action" href="#sec-alerts" style="font-size:12px;color:var(--accent-dark);font-weight:600;text-decoration:none">查看异常清单 →</a>`;
}

/* ═══════════════ KPI ═══════════════ */
function renderKpi(){
  const rows=filtered(),prev=filtered(null,'prev');
  const cost=sum(rows,r=>r.est_cost_usd),uc=usersOf(rows),puc=usersOf(prev);
  const hc=sum(rows.filter(isHigh),r=>r.est_cost_usd),hp=cost>0?hc/cost*100:0;
  const pCost=sum(prev,r=>r.est_cost_usd);
  const inT=sum(rows,r=>r.prompt_tokens),cached=sum(rows,r=>r.cached_tokens),cacheRate=cached+inT>0?cached/(cached+inT)*100:0;
  const calls=sum(rows,r=>r.call_count),cpc=calls>0?cost/calls:0;
  const pInT=sum(prev,r=>r.prompt_tokens),pCached=sum(prev,r=>r.cached_tokens),pCacheRate=pCached+pInT>0?pCached/(pCached+pInT)*100:0;
  const pCalls=sum(prev,r=>r.call_count),pCpc=pCalls>0?pCost/pCalls:0;

  const setDelta=(id,delta)=>{const el=document.getElementById(id),a=Math.abs(delta);if(!isFinite(a)||a<0.5)el.outerHTML=`<span class="delta flat" id="${id}">持平</span>`;else{const dir=delta>0?'up':'down';el.outerHTML=`<span class="delta ${dir}" id="${id}"><span>${delta>0?'↑':'↓'} ${a.toFixed(1)}%</span><span class="dnote">vs 上区间</span></span>`;}};

  const el=document.getElementById('kpi-cost-val');el.textContent=fmtC(cost);
  const budget=BUDGET_PER_DAY*CURR_DAYS.length;
  setDelta('kpi-cost-delta',deltaPct(cost,pCost));
  document.getElementById('kpi-cost-sub').textContent=`共 ${uc} 人 · 日均 ${fmtC(cost/CURR_DAYS.length)}`;
  const bt=document.getElementById('budget-txt');bt.textContent=`${fmtC(cost)} / ${fmtC(budget)}`;
  const bf=document.getElementById('budget-fill');const bp=budget>0?Math.min(100,cost/budget*100):0;
  bf.style.width=bp+'%';bf.className='budget-fill'+(bp>=100?' over':bp>=80?' warn':'');

  const av=document.getElementById('kpi-avg-val');av.textContent=uc>0?fmtC(cost/uc):'–';
  setDelta('kpi-avg-delta',uc>0&&puc>0?deltaPct(cost/uc,pCost/puc):0);
  document.getElementById('kpi-avg-sub').textContent=`均值会被头部用户拉高 · 见部门 vs 中位`;

  const cv=document.getElementById('kpi-cache-val');cv.textContent=cacheRate.toFixed(1)+'%';
  setDelta('kpi-cache-delta',deltaPct(cacheRate,pCacheRate));

  const cw=document.getElementById('kpi-cpc-val');cw.textContent='$'+cpc.toFixed(4);
  setDelta('kpi-cpc-delta',deltaPct(cpc,pCpc));

  renderVerdict(cost,pCost,hp,budget);
  renderAlerts(rows,cost);
  renderTrend(rows,prev);
  renderHiCost(rows);
  renderModels(rows);
  renderInsight();
}
function renderAlerts(rows,totalCost){
  const list=buildAlerts(rows,totalCost),el=document.getElementById('alertStrip');
  el.innerHTML=list.length?list.map(a=>`<div class="alert ${a.lvl}"><span class="dot"></span><span>${a.txt}</span>${a.act?`<span class="act">${a.act}</span>`:''}</div>`).join(''):'';
}
/* ═══════════════ AI 分析卡（原型：确定性规则引擎，非真 LLM）═══════════════
   三段式：事实 → 原因 → 建议，每条锚定真实数字，带下钻路径。
   随筛选联动：只看一个部门/模型/等级时，分析范围收窄到该对象（全司中位数始终为全局参照）。
   只引用实时算出的真实数字（估算值明确标注"估"）。
   后端将来只需一个 /analytics/insight 端点（输入区间汇总+信号，输出洞察文本）。 */
function currentScope(){
  const m=state.models,t=state.tiers,d=state.depts;
  if(m.size===1&&!t.size&&!d.size)return{kind:'model',key:[...m][0],label:'模型：'+[...m][0]};
  if(d.size===1&&!m.size&&!t.size)return{kind:'dept',key:[...d][0],label:'部门：'+[...d][0]};
  if(t.size===1&&!m.size&&!d.size)return{kind:'tier',key:[...t][0],label:'等级：'+TIER_LABEL[[...t][0]]};
  return{kind:'company',key:null,label:'全司'};
}
function drillHtml(drill){
  if(!drill)return '';
  const a=drill.dept?`data-dept="${drill.dept}" `:'';
  const b=drill.model?`data-model="${drill.model}" `:'';
  const s=drill.scroll||'overview';
  return ` <a class="ai-drill" ${a}${b}data-scroll="${s}">下钻</a>`;
}
function renderInsight(){
  const el=document.getElementById('aiInsight');if(!el)return;
  const scope=currentScope(),rows=filtered(),prev=filtered(null,'prev');
  const allCurr=ALL_ROWS.filter(r=>r.period==='curr');
  const gmed=median(Object.keys(by(r=>r.user_id,allCurr)).map(u=>sum(by(r=>r.user_id,allCurr)[u],r=>r.est_cost_usd)));
  const F=[],W=[],S=[];
  if(!rows.length){el.innerHTML=`<div class="ai-hd"><div class="ai-avatar">AI</div><div class="ai-name">本周要点</div><span class="ai-badge">${scope.label}</span></div><div class="ai-body" style="color:var(--text-3)">当前范围无数据，调整筛选后再看分析。</div>`;return;}

  const cost=sum(rows,r=>r.est_cost_usd),pCost=sum(prev,r=>r.est_cost_usd);
  const delta=deltaPct(cost,pCost),absD=Math.abs(delta);
  const budget=BUDGET_PER_DAY*CURR_DAYS.length,budPct=cost>0?cost/budget*100:0;
  const hc=sum(rows.filter(isHigh),r=>r.est_cost_usd),hp=cost>0?hc/cost*100:0;
  const inT=sum(rows,r=>r.prompt_tokens),cached=sum(rows,r=>r.cached_tokens),cacheRate=cached+inT>0?cached/(cached+inT)*100:0;
  const bm=by(r=>r.model,rows),pbm=by(r=>r.model,prev);
  const modelArr=Object.keys(bm).map(m=>({m,c:sum(bm[m],r=>r.est_cost_usd),pc:sum((pbm[m]||[]),r=>r.est_cost_usd)})).map(x=>({...x,d:x.c-x.pc,pct:x.pc>0?(x.c-x.pc)/x.pc*100:0})).sort((a,b)=>b.c-a.c);
  const topM=modelArr[0],mover=[...modelArr].sort((a,b)=>Math.abs(b.d)-Math.abs(a.d))[0];
  const dr=by(r=>r.dept_name,rows);
  const deptRanks=Object.keys(dr).map(d=>({d,c:sum(dr[d],r=>r.est_cost_usd),du:usersOf(dr[d])})).sort((a,b)=>b.c-a.c);
  const topD=deptRanks[0],topDAvg=topD.c/topD.du,ratio=gmed>0?topDAvg/gmed:0;
  const ur=by(r=>r.user_id,rows);
  const topU=Object.keys(ur).map(u=>({u,row:ur[u][0],c:sum(ur[u],r=>r.est_cost_usd)})).sort((a,b)=>b.c-a.c)[0];
  const topUShare=cost>0?topU.c/cost*100:0;
  const uM=by(r=>r.model,ur[topU.u]);
  const topUM=Object.keys(uM).map(m=>({m,c:sum(uM[m],r=>r.est_cost_usd)})).sort((a,b)=>b.c-a.c)[0];
  const topUDeptTot=sum((dr[topU.row.dept_name]||[]),r=>r.est_cost_usd);
  const topUDeptShare=topUDeptTot>0?topU.c/topUDeptTot*100:0;
  const saveEst=tierOf(topUM.m)==='L3-Premium'?topUM.c*0.3:0;
  const hr={};ur[topU.u].forEach(r=>{hr[r.hour]=(hr[r.hour]||0)+r.est_cost_usd;});
  const peakH=Object.keys(hr).map(h=>[+h,hr[h]]).sort((a,b)=>b[1]-a[1])[0];

  // ── 事实 ──
  if(scope.kind==='company'){
    F.push(`区间费用 <b>${fmtC(cost)}</b> / 预算 <b>${budPct.toFixed(0)}%</b> / 环比 <b class="num">${delta>=0?'+':''}${absD.toFixed(1)}%</b>，高阶模型（L3）占 <b>${hp.toFixed(0)}%</b>。`);
    F.push(`费用大头在 <b>${topM.m}</b>（${TIER_LABEL[tierOf(topM.m)]}，<span class="num">${fmtC(topM.c)}</span>，占 <span class="num">${(topM.c/cost*100).toFixed(0)}%</span>）${drillHtml({scroll:'overview',model:topM.m})}。`);
    if(ratio>=2)F.push(`<b>${topD.d}</b> 人均 <span class="num">${fmtC(topDAvg)}</span>，超全司中位数 <span class="num">${ratio.toFixed(1)}x</span>${drillHtml({scroll:'dept',dept:topD.d})}；<b>${topU.row.user_name}</b> 占全司 <span class="num">${topUShare.toFixed(0)}%</span>。`);
  }else if(scope.kind==='dept'){
    F.push(`<b>${scope.label.split('：')[1]}</b> 区间费用 <b>${fmtC(cost)}</b> / 环比 <b class="num">${delta>=0?'+':''}${absD.toFixed(1)}%</b> / 高阶模型占 <b>${hp.toFixed(0)}%</b>。`);
    const uavg=cost/usersOf(rows),uratio=gmed>0?uavg/gmed:0;
    F.push(`本部门费用大头在 <b>${topM.m}</b>（占 <span class="num">${(topM.c/cost*100).toFixed(0)}%</span>）${drillHtml({scroll:'overview',model:topM.m})}。`);
    F.push(`部门人均 <span class="num">${fmtC(uavg)}</span>，超全司中位数 <span class="num">${uratio.toFixed(1)}x</span>；<b>${topU.row.user_name}</b> 一人占本部门 <span class="num">${topUDeptShare.toFixed(0)}%</span>${drillHtml({scroll:'dept',dept:scope.key})}。`);
  }else{
    F.push(`<b>${scope.label.split('：')[1]}</b> 区间费用 <b>${fmtC(cost)}</b> / 环比 <b class="num">${delta>=0?'+':''}${absD.toFixed(1)}%</b> / 缓存命中 <b>${cacheRate.toFixed(1)}%</b>。`);
    if(topU)F.push(`由 <b>${topU.row.user_name}</b>（${topU.row.dept_name}）消耗最多，占该维度费用 <span class="num">${topUShare.toFixed(0)}%</span>${drillHtml({scroll:'users'})}。`);
  }

  // ── 原因 ──
  if(scope.kind==='company'){
    W.push(`环比${delta>=0?'上涨':'下降'} <b class="num">${absD.toFixed(1)}%</b>，主因 <b>${mover.m}</b>（区间 <span class="num">${mover.d>=0?'+':'−'}${fmtC(Math.abs(mover.d))}</span>，该模型环比 <span class="num">${mover.pct>=0?'+':''}${mover.pct.toFixed(1)}%</span>）${drillHtml({scroll:'overview',model:mover.m})}。`);
    if(ratio>=2)W.push(`<b>${topD.d}</b> 人均偏高的主因是 <b>${topU.row.user_name}</b> 一人占部门费用 <span class="num">${topUDeptShare.toFixed(0)}%</span>，且其调用 <span class="num">${(topUM.c/topU.c*100).toFixed(0)}%</span> 集中在 <b>${topUM.m}</b>（${TIER_LABEL[tierOf(topUM.m)]}）${drillHtml({scroll:'dept',dept:topD.d})}。`);
    W.push(`<b>${topU.row.user_name}</b> 的调用集中在工作时段，<span class="num">${String(peakH[0]).padStart(2,'0')}:00</span> 单小时达 <span class="num">${fmtC(peakH[1])}</span>，峰值明显放大。`);
  }else if(scope.kind==='dept'){
    W.push(`本部门环比${delta>=0?'上涨':'下降'} <b class="num">${absD.toFixed(1)}%</b>，主因 <b>${mover.m}</b>（区间 <span class="num">${mover.d>=0?'+':'−'}${fmtC(Math.abs(mover.d))}</span>）${drillHtml({scroll:'overview',model:mover.m})}。`);
    if(topUDeptShare>=40)W.push(`人均偏高的主因是 <b>${topU.row.user_name}</b> 一人占部门 <span class="num">${topUDeptShare.toFixed(0)}%</span>，其调用 <span class="num">${(topUM.c/topU.c*100).toFixed(0)}%</span> 集中在 <b>${topUM.m}</b>（${TIER_LABEL[tierOf(topUM.m)]}）。`);
    W.push(`<b>${topU.row.user_name}</b> 高峰在 <span class="num">${String(peakH[0]).padStart(2,'0')}:00</span>，单小时 <span class="num">${fmtC(peakH[1])}</span>。`);
  }else{
    const bd=by(r=>r.dept_name,rows),pbd=by(r=>r.dept_name,prev);
    const deptArr=Object.keys(bd).map(d=>({d,c:sum(bd[d],r=>r.est_cost_usd),pc:sum((pbd[d]||[]),r=>r.est_cost_usd)})).map(x=>({...x,dv:x.c-x.pc})).sort((a,b)=>Math.abs(b.dv)-Math.abs(a.dv));
    const dMover=deptArr[0];
    if(dMover)W.push(`该维度环比${delta>=0?'上涨':'下降'} <b class="num">${absD.toFixed(1)}%</b>，主因 <b>${dMover.d}</b>（区间 <span class="num">${dMover.dv>=0?'+':'−'}${fmtC(Math.abs(dMover.dv))}</span>）${drillHtml({scroll:'dept',dept:dMover.d})}。`);
    W.push(`<b>${topU.row.user_name}</b> 高峰在 <span class="num">${String(peakH[0]).padStart(2,'0')}:00</span>，单小时 <span class="num">${fmtC(peakH[1])}</span>。`);
  }

  // ── 建议 ──
  if(saveEst>0)S.push(`对 <b>${topU.row.dept_name}</b> / <b>${topU.row.user_name}</b> 非关键链路的 <b>${topUM.m}</b> 调用降至 L2/L1，按 <b>30% 可降级</b>估，区间约省 <span class="num">${fmtC(saveEst)}</span><span class="ai-flag est">估</span>${drillHtml({scroll:'dept',dept:topU.row.dept_name})}。`);
  if(cacheRate<40)S.push(`整体缓存命中率仅 <span class="num">${cacheRate.toFixed(1)}%</span>，对高频重复输入开启 prompt 缓存可压降输入费用。`);
  S.push(`下钻 <b>${topU.row.user_name}</b>（${topU.row.dept_name}）${CURR_DAYS[0].slice(5)}~${CURR_DAYS[CURR_DAYS.length-1].slice(5)} 小时级明细，确认 ${String(peakH[0]).padStart(2,'0')}:00 峰值调用是否可控 / 可回收${drillHtml({scroll:'dept',dept:topU.row.dept_name})}。`);

  el.innerHTML=`<div class="ai-hd"><div class="ai-avatar">AI</div><div class="ai-name">本周要点</div><span class="ai-badge">${scope.label}</span><span class="ai-badge">规则引擎 · 引用真实数字</span></div>
  <div class="ai-body">
    <div class="ai-sec"><span class="ai-sec-tag">事实</span>${F.join(' ')}</div>
    <div class="ai-sec"><span class="ai-sec-tag">原因</span>${W.join(' ')}</div>
    <div class="ai-sec"><span class="ai-sec-tag">建议</span>${S.join(' ')}</div>
  </div>
  <div class="ai-foot">以上要点由确定性规则引擎生成（当前为原型模拟 AI）· 分析范围随筛选联动 · 所有引用数值均可下钻到行级核实 · 带"估"字为估算 · 真实接入时将替换为 LLM 洞察</div>`;
  el.onclick=function(e){
    const a=e.target.closest('.ai-drill');if(!a)return;
    if(a.dataset.dept)openDept=a.dataset.dept;
    if(a.dataset.model){openModel=a.dataset.model;detailDay='all';}
    renderAll();
    const sec=document.getElementById('sec-'+(a.dataset.scroll||'overview'));
    if(sec)sec.scrollIntoView({behavior:'smooth',block:'start'});
  };
}
function buildAlerts(rows,totalCost){
  const list=[],dr=by(r=>r.dept_name,rows);
  const perUserCosts=[],duM={};for(const u of Object.keys(by(r=>r.user_id,rows))){const r=by(r=>r.user_id,rows)[u],c=sum(r,r=>r.est_cost_usd);perUserCosts.push(c);duM[u]=c;}
  const med=median(perUserCosts);
  for(const d of Object.keys(dr)){const r=dr[d],c=sum(r,r=>r.est_cost_usd),du=usersOf(r);if(!du)continue;const a=c/du;if(med>0&&a/med>3)list.push({lvl:'danger',txt:`<b>${d}</b> 人均 ${fmtC(a)}，超全司中位数 ${(a/med).toFixed(1)}x`,act:'下钻 →'});else if(med>0&&a/med>2)list.push({lvl:'warn',txt:`<b>${d}</b> 人均 ${fmtC(a)}，超全司中位数 ${(a/med).toFixed(1)}x`,act:'下钻 →'});}
  const ur=by(r=>r.user_id,rows);for(const u of Object.keys(ur)){const r=ur[u],c=sum(r,r=>r.est_cost_usd);if(c<=0)continue;const hc=sum(r.filter(isHigh),r=>r.est_cost_usd),p=hc/c*100;if(p>=80&&c>=totalCost*.05)list.push({lvl:'warn',txt:`<b>${r[0].user_name}</b>（${r[0].dept_name}）高阶占比 ${p.toFixed(0)}%，费用 ${fmtC(c)}`});}
  return list;
}

/* ═══════════════ 费用趋势 · 日级（前段灰=上一区间 · 后段堆叠=当前区间）═══════════════ */
function renderTrend(rows,prev){
  destroyCharts(trendChart);trendChart=null;
  const preAgg=by(r=>r.date,prev),dayOrder=[...PREV_DAYS,...CURR_DAYS];
  const curAgg=by(r=>r.date,rows);
  const labels=dayOrder.map(d=>d.slice(5));
  const prevData=dayOrder.map(d=>preAgg[d]?Math.round(sum(preAgg[d],r=>r.est_cost_usd)*100)/100:null);
  const modelKeys=MODELS.map(m=>m.id);
  const datasets=[];
  datasets.push({label:'上一区间费用',data:prevData,borderColor:'#CBD5E1',backgroundColor:'rgba(148,163,184,.14)',fill:true,pointRadius:0,borderWidth:1.5,borderDash:[4,3],yAxisID:'y'});
  for(const m of modelKeys){const rowsM=(curAgg?Object.values(curAgg):[]).flat().filter(r=>r.model===m);const am={};for(const r of rowsM)am[r.date]=(am[r.date]||0)+r.est_cost_usd;const data=dayOrder.map(d=>CURR_DAYS.includes(d)?Math.round((am[d]||0)*100)/100:null);if(data.some(v=>v))datasets.push({label:TIER_LABEL[tierOf(m)]==='未分级'?m:m,data,borderColor:'transparent',backgroundColor:TIER_HEX[tierOf(m)]+'cc',stack:'cur',barPercentage:.6,categoryPercentage:.7,order:1,yAxisID:'y'});}
  const cv=document.getElementById('trendChart');
  if(!datasets.length||!datasets.slice(1).length){cv.parentElement.innerHTML='<div class="meta" style="padding:60px;text-align:center">暂无数据</div>';return;}
  trendChart=new Chart(cv,{type:'bar',data:{labels,datasets},options:{responsive:true,maintainAspectRatio:false,interaction:{intersect:false,mode:'index'},plugins:{legend:{...baseLegend(),labels:{...baseLegend().labels,filter:item=>item.text!=='上一区间费用'}}},scales:{x:{stacked:true,grid:{display:false},ticks:{color:axCol(),font:{size:11}}},y:{beginAtZero:true,grid:{color:gridCol()},ticks:{color:axCol(),font:{size:10},callback:v=>'$'+v}}}}});
}

/* ═══════════════ 部门 × 高阶费用 ═══════════════ */
function renderHiCost(rows){
  const dr=by(r=>r.dept_name,rows),el=document.getElementById('hiCostList');
  const arr=DEPTS.map(d=>({d,rows:dr[d]||[]})).filter(x=>x.rows.length).map(x=>{const hi=sum(x.rows.filter(isHigh),r=>r.est_cost_usd),tot=sum(x.rows,r=>r.est_cost_usd);return{d:x.d,hi,tot,pct:tot>0?hi/tot*100:0};}).sort((a,b)=>b.hi-a.hi);
  if(!arr.length){el.innerHTML='<div class="meta" style="padding:30px;text-align:center">暂无数据</div>';return;}
  const max=Math.max(1,arr[0].hi);let html='';
  for(const x of arr){const pct=x.pct,pLabel=pct>=80?'<span class="t-red">':pct>=60?'<span class="t-amber">':'<span class="t-green">';html+=`<div class="hi-row"><div class="hi-lbl">${x.d}</div><div class="hi-track"><div class="hi-fill" style="width:${Math.max(2,x.hi/max*100)}%;background:${pct>=80?'var(--red)':pct>=60?'var(--amber)':'var(--green)'}"></div></div><div class="hi-val"><b>${fmtC(x.hi)}</b><span class="t-muted">${pLabel}${pct.toFixed(0)}%</span></div></div>`;}
  el.innerHTML=html;
}

/* ═══════════════ 模型费用表（点击展开 24h 明细）═══════════════ */
function renderModels(rows){
  const bm=by(r=>r.model,rows),prevBm=by(r=>r.model,filtered(null,'prev')),el=document.getElementById('modelList');
  const arr=MODELS.map(m=>({m,rows:bm[m.id]||[]})).filter(x=>x.rows.length).map(x=>{const c=sum(x.rows,r=>r.est_cost_usd),pc=sum((prevBm[x.m.id]||[]),r=>r.est_cost_usd);return{m:x.m.id,tier:tierOf(x.m.id),cost:c,delta:deltaPct(c,pc),calls:sum(x.rows,r=>r.call_count),cacheRate:(()=>{const t=sum(x.rows,r=>r.prompt_tokens),k=sum(x.rows,r=>r.cached_tokens);return t+k>0?k/(t+k)*100:0;})()};}).sort((a,b)=>b.cost-a.cost);
  if(openModel===null&&!defaultedModel){openModel=arr[0].m;defaultedModel=true;}
  let html='';
  for(const r of arr){const tc=r.tier==='unclassified'?'un':r.tier.split('-')[0].toLowerCase();const del=r.delta,delCls=Math.abs(del)<0.5?'flat':del>0?'up':'down',delTxt=Math.abs(del)<0.5?'持平':(del>0?'↑':'↓')+Math.abs(del).toFixed(1)+'%';html+=`<div class="model-row ${openModel===r.m?'open':''}" data-m="${r.m}"><div class="m-name"><span class="nm">${r.m}</span><span class="tag tag-${tc}">${TIER_LABEL[r.tier]}</span></div><div class="m-num m-col-cost"><b>${fmtC(r.cost)}</b></div><div class="m-num m-col-delta"><span class="delta ${delCls}">${delTxt}</span></div><div class="m-num m-col-metric">cache <b>${r.cacheRate.toFixed(0)}%</b></div><div class="m-num m-col-metric">${fmtN(r.calls)} 次调用</div><span class="m-caret">${openModel===r.m?'▾':'▸'}</span></div>`;}
  el.innerHTML=html;
  el.querySelectorAll('.model-row').forEach(row=>row.addEventListener('click',()=>{openModel=openModel===row.dataset.m?null:row.dataset.m;detailDay='all';renderModels(rows);renderModelDetail();}));
  renderModelDetail(rows);
}
function renderModelDetail(rows){
  const el=document.getElementById('modelDetail');if(!openModel){el.className='model-detail';el.innerHTML='';return;}
  const elRows=rows.filter(r=>r.model===openModel);
  el.className='model-detail open';
  const tc=tierOf(openModel),tcl=tc==='unclassified'?'un':tc.split('-')[0].toLowerCase();
  const daySet=CURR_DAYS.filter(d=>elRows.some(r=>r.date===d));
  const dayChips=daySet.map(d=>`<span class="chip ${detailDay===d?'on':''}" data-dd="${d}">${d.slice(5)}</span>`).join('');
  const rowsSel=detailDay==='all'?elRows:elRows.filter(r=>r.date===detailDay);
  const cstSel=sum(rowsSel,r=>r.est_cost_usd);
  const inT=sum(rowsSel,r=>r.prompt_tokens),cached=sum(rowsSel,r=>r.cached_tokens),outT=sum(rowsSel,r=>r.completion_tokens);
  el.innerHTML=`<div class="detail-title"><span class="tag tag-${tcl}">${TIER_LABEL[tc]}</span> ${openModel} · 24h 小时级明细（${fmtC(cstSel)} / ${fmtN(inT+cached)} in · ${fmtN(outT)} out）</div>
  <div class="chip-group" style="margin-bottom:12px"><span class="chip ${detailDay==='all'?'on':''}" data-dd="all">全部</span>${dayChips}</div>
  <div class="grid-3">
    <div><div class="card-hd" style="margin-bottom:8px"><div><h3 style="font-size:13.5px">小时费用</h3><div class="meta">堆积 = 每小时费用（${fmtC(cstSel)}）</div></div></div><div class="chart-box" style="height:200px"><canvas id="detailStack"></canvas></div></div>
    <div><div class="card-hd" style="margin-bottom:8px"><div><h3 style="font-size:13.5px">小时 Token</h3><div class="meta">输入 / 缓存 / 输出 三色堆叠</div></div></div><div class="chart-box" style="height:200px"><canvas id="detailTok"></canvas></div></div>
    <div><div class="card-hd" style="margin-bottom:8px"><div><h3 style="font-size:13.5px">小时费用热力</h3><div class="meta">24 小时 · 数值越高越深</div></div></div><div class="heatmap" id="detailHeat"></div></div>
  </div>`;
  // 小时堆叠（费用）
  destroyCharts(detailStackChart);detailStackChart=null;
  const hrs={};for(const r of rowsSel){const h=r.hour;if(!hrs[h])hrs[h]=0;hrs[h]+=r.est_cost_usd;}
  const hL=Array.from({length:24},(_,h)=>String(h).padStart(2,'0')+':00');
  const scv=document.getElementById('detailStack');
  detailStackChart=new Chart(scv,{type:'bar',data:{labels:hL,datasets:[{data:Array.from({length:24},(_,i)=>hrs[i]||0),backgroundColor:TIER_HEX[tc]+'cc',borderRadius:2}]},options:{responsive:true,maintainAspectRatio:false,plugins:{legend:{display:false},tooltip:{...TOOLTIP_STYLE,callbacks:{label:c=>' '+fmtC(c.parsed.y)}}},scales:{x:{grid:{display:false},ticks:{color:axCol(),font:{size:9},maxTicksLimit:12}},y:{beginAtZero:true,grid:{color:gridCol()},ticks:{color:axCol(),font:{size:9},callback:v=>'$'+v}}}}});
  // 小时 Token（输入/缓存/输出 堆叠）
  destroyCharts(detailTokChart);detailTokChart=null;
  const tok=rowsSel.reduce((m,r)=>{m.p[r.hour]=(m.p[r.hour]||0)+r.prompt_tokens;m.c[r.hour]=(m.c[r.hour]||0)+r.cached_tokens;m.o[r.hour]=(m.o[r.hour]||0)+r.completion_tokens;return m;},{p:{},c:{},o:{}});
  const tcv=document.getElementById('detailTok');
  detailTokChart=new Chart(tcv,{type:'bar',data:{labels:hL,datasets:[
    {label:'输入',data:Array.from({length:24},(_,i)=>tok.p[i]||0),backgroundColor:TOKEN_HEX.prompt_tokens+'99',stack:'t',borderRadius:2},
    {label:'缓存',data:Array.from({length:24},(_,i)=>tok.c[i]||0),backgroundColor:TOKEN_HEX.cached_tokens+'99',stack:'t',borderRadius:2},
    {label:'输出',data:Array.from({length:24},(_,i)=>tok.o[i]||0),backgroundColor:TOKEN_HEX.completion_tokens+'99',stack:'t',borderRadius:2}
  ]},options:{responsive:true,maintainAspectRatio:false,plugins:{legend:{...baseLegend(),labels:{...baseLegend().labels,boxWidth:8,font:{size:10}}},tooltip:{...TOOLTIP_STYLE,callbacks:{label:c=>` ${c.dataset.label} ${fmtN(c.parsed.y)}`}}},scales:{x:{stacked:true,grid:{display:false},ticks:{color:axCol(),font:{size:9},maxTicksLimit:12}},y:{stacked:true,beginAtZero:true,grid:{color:gridCol()},ticks:{color:axCol(),font:{size:9},callback:v=>fmtN(v)}}}}});
  // 小时热力
  const heatCell={};for(const r of rowsSel){heatCell[r.hour]=(heatCell[r.hour]||0)+r.est_cost_usd;}
  const max=Math.max(1,...Object.values(heatCell)),hm=document.getElementById('detailHeat');
  const hc=v=>{if(v<=0)return(isDark?HEAT_DARK:HEAT_LIGHT)[0];const r=v/max;if(r<.25)return(isDark?HEAT_DARK:HEAT_LIGHT)[1];if(r<.5)return(isDark?HEAT_DARK:HEAT_LIGHT)[2];if(r<.75)return(isDark?HEAT_DARK:HEAT_LIGHT)[3];return(isDark?HEAT_DARK:HEAT_LIGHT)[4];};
  let hh='';for(let h=0;h<24;h++){const v=heatCell[h]||0;hh+=`<div class="h-cell" style="background:${hc(v)}" title="${String(h).padStart(2,'0')}:00 · ${fmtC(v)}"></div>`;}
  hm.innerHTML=hh;
  el.querySelectorAll('[data-dd]').forEach(c=>c.addEventListener('click',()=>{detailDay=c.dataset.dd;renderModelDetail(rows);}));
}

/* ═══════════════ 部门透视 ═══════════════ */
function renderDept(){
  const rows=filtered(),deptRows=by(r=>r.dept_name,rows),prevRows=filtered(null,'prev'),prevDept=by(r=>r.dept_name,prevRows),body=document.getElementById('deptBody');body.innerHTML='';
  const perUser=[];for(const u of Object.keys(by(r=>r.user_id,rows)))perUser.push(sum(by(r=>r.user_id,rows)[u],r=>r.est_cost_usd));
  const med=median(perUser);
  const ra=DEPTS.map(d=>({d,rows:deptRows[d]||[]})).filter(x=>x.rows.length);ra.sort((a,b)=>sum(b.rows,r=>r.est_cost_usd)-sum(a.rows,r=>r.est_cost_usd));
  document.getElementById('deptNote').textContent=period==='month'?'按月汇总 · 人均 vs 全司中位数 · 点击部门展开用户明细':'按天汇总 · 人均 vs 全司中位数 · 点击部门展开用户明细';
  for(const{d,rows:dr}of ra){const cst=sum(dr,r=>r.est_cost_usd),pcst=sum(prevDept[d]||[],r=>r.est_cost_usd),du=usersOf(dr),avg=du?cst/du:0,ratio=med>0?avg/med:0,hc=sum(dr.filter(isHigh),r=>r.est_cost_usd),hpct=cst>0?hc/cst*100:0;
    const del=deltaPct(cst,pcst),delCls=Math.abs(del)<0.5?'flat':del>0?'up':'down',delTxt=Math.abs(del)<0.5?'持平':(del>0?'↑':'↓')+Math.abs(del).toFixed(0)+'%';
    let warn='';if(ratio>3)warn='<span class="badge badge-red">人均超标</span>';else if(ratio>2)warn='<span class="badge badge-amber">人均偏高</span>';
    if(hpct>=80)warn+=' <span class="badge badge-red">高阶高</span>';else if(hpct>=60)warn+=' <span class="badge badge-amber">高阶偏高</span>';
    const rl=ratio>3?'<span class="t-red">':ratio>2?'<span class="t-amber">':'<span class="t-green">';
    const hp=hpct>=80?'<span class="t-red">':hpct>=60?'<span class="t-amber">':'<span class="t-green">';
    const tr=document.createElement('tr');tr.style.cursor='pointer';
    tr.innerHTML=`<td class="t-link">${d} ${openDept===d?'▾':'▸'}</td><td class="num cost">${fmtC(cst)}</td><td class="num"><span class="delta ${delCls}">${delTxt}</span></td><td class="num">${du}</td><td class="num">${fmtC(avg)}</td><td class="num">${rl}${ratio.toFixed(1)}x</span> <span class="t-dim">中位</span></td><td class="num"><span class="bar"><span class="bar-track"><span class="bar-fill" style="width:${Math.min(hpct,100)}%;background:${hpct>=80?'var(--red)':hpct>=60?'var(--amber)':'var(--green)'}"></span></span><span class="t-muted">${hp}${hpct.toFixed(0)}%</span></span></td><td>${warn||'<span class="t-muted">—</span>'}</td>`;
    tr.addEventListener('click',()=>{openDept=openDept===d?null:d;renderDept();});body.appendChild(tr);}
  if(!ra.length)body.innerHTML='<tr><td colspan="8" style="text-align:center;color:var(--text-3);padding:20px">暂无数据</td></tr>';
  renderDeptDrill(ra);
}
function renderDeptDrill(ra){
  const el=document.getElementById('deptDrill');if(!openDept){el.innerHTML='';return;}const entry=ra.find(x=>x.d===openDept);if(!entry){el.innerHTML='';return;}const dr=entry.rows,am={};
  for(const r of dr){if(!am[r.user_id])am[r.user_id]={user_name:r.user_name,user_id:r.user_id,prompt_tokens:0,cached_tokens:0,completion_tokens:0,est_cost_usd:0,high:0,call_count:0};const u=am[r.user_id];u.prompt_tokens+=r.prompt_tokens;u.cached_tokens+=r.cached_tokens;u.completion_tokens+=r.completion_tokens;u.est_cost_usd+=r.est_cost_usd;u.call_count+=r.call_count;if(isHigh(r))u.high+=r.est_cost_usd;}
  const rows=Object.values(am).sort((a,b)=>b.est_cost_usd-a.est_cost_usd),mTotal=sum(rows,r=>r.est_cost_usd),perHd=period==='month'?`按月（${monthOf(CURR_DAYS[0])}）`:'按天';
  let h=[`<div class="drill"><h4>${openDept} · 用户明细（${rows.length} 人 · ${perHd}）</h4><table><thead><tr><th>用户</th><th class="num">费用</th><th class="num">$/调用</th><th class="num">cache</th><th class="num">占比</th><th class="num">高阶占比</th><th class="num">调用</th></tr></thead><tbody>`];
  for(const r of rows){const pct=mTotal>0?r.est_cost_usd/mTotal*100:0,cp=r.call_count>0?r.est_cost_usd/r.call_count:0,cr=r.prompt_tokens+r.cached_tokens>0?r.cached_tokens/(r.prompt_tokens+r.cached_tokens)*100:0,hpct=r.est_cost_usd>0?r.high/r.est_cost_usd*100:0,hb=hpct>=80?'<span class="badge badge-red">高阶</span>':hpct>=60?'<span class="badge badge-amber">偏高</span>':'';h.push(`<tr><td style="font-weight:600">${r.user_name} ${hb}</td><td class="num cost">${fmtC(r.est_cost_usd)}</td><td class="num">${cp.toFixed(4)}</td><td class="num">${cr.toFixed(0)}%</td><td class="num"><span class="bar"><span class="bar-track"><span class="bar-fill" style="width:${Math.min(pct,100)}%"></span></span><span class="t-muted">${pct.toFixed(0)}%</span></span></td><td class="num">${hpct>=80?'<span class="t-red">':hpct>=60?'<span class="t-amber">':'<span class="t-green">'}${hpct.toFixed(0)}%</span></td><td class="num t-muted">${fmtN(r.call_count)}</td></tr>`);}
  h.push('</tbody></table></div>');el.innerHTML=h.join('');
}

/* ═══════════════ 用户排行 ═══════════════ */
function renderUsers(){
  const rows=filtered(),um={};
  for(const r of rows){if(!um[r.user_id])um[r.user_id]={user_id:r.user_id,user_name:r.user_name,dept_name:r.dept_name,prompt_tokens:0,cached_tokens:0,completion_tokens:0,est_cost_usd:0,high:0,call_count:0,prevCost:0};const u=um[r.user_id];u.prompt_tokens+=r.prompt_tokens;u.cached_tokens+=r.cached_tokens;u.completion_tokens+=r.completion_tokens;u.est_cost_usd+=r.est_cost_usd;u.call_count+=r.call_count;if(isHigh(r))u.high+=r.est_cost_usd;}
  const prevRows=filtered(null,'prev');for(const r of prevRows){const u=um[r.user_id];if(u)u.prevCost+=r.est_cost_usd;}
  const arr=Object.values(um).sort((a,b)=>b.est_cost_usd-a.est_cost_usd),totalCost=sum(arr,r=>r.est_cost_usd),show=arr.slice(0,userShowAll?20:10),body=document.getElementById('userBody');body.innerHTML='';
  const more=document.getElementById('userMore');more.style.display=arr.length>10?'':'none';more.textContent=userShowAll?'收起':'展开全部';
  const perHd=period==='month'?`按月（${monthOf(CURR_DAYS[0])}）`:'按天';
  document.getElementById('userHead').innerHTML=`<tr><th>#</th><th>用户</th><th>部门</th><th class="num">费用</th><th class="num">环比</th><th class="num">$/调用</th><th class="num">cache 命中</th><th class="num">高阶占比</th><th class="num">调用</th><th class="num">费用占比</th></tr>`;
  document.getElementById('userNote').textContent=arr.length?`${arr.length} 人 · 按费用降序 · ${perHd}`:'';
  show.forEach((u,i)=>{const pct=totalCost>0?u.est_cost_usd/totalCost*100:0,hpct=u.est_cost_usd>0?u.high/u.est_cost_usd*100:0,cr=u.prompt_tokens+u.cached_tokens>0?u.cached_tokens/(u.prompt_tokens+u.cached_tokens)*100:0,cp=u.call_count>0?u.est_cost_usd/u.call_count:0,del=deltaPct(u.est_cost_usd,u.prevCost),delCls=Math.abs(del)<0.5?'flat':del>0?'up':'down',delTxt=Math.abs(del)<0.5?'持平':(del>0?'↑':'↓')+Math.abs(del).toFixed(0)+'%';const tr=document.createElement('tr');
    tr.innerHTML=`<td class="t-muted">${i+1}</td><td style="font-weight:600">${u.user_name}</td><td><span class="badge badge-green">${u.dept_name}</span></td><td class="num cost">${fmtC(u.est_cost_usd)}</td><td class="num"><span class="delta ${delCls}">${delTxt}</span></td><td class="num">${cp.toFixed(4)}</td><td class="num">${cr.toFixed(0)}%</td><td class="num">${hpct>=80?'<span class="t-red">':hpct>=60?'<span class="t-amber">':'<span class="t-green">'}${hpct.toFixed(0)}%</span></td><td class="num t-muted">${fmtN(u.call_count)}</td><td class="num"><span class="bar"><span class="bar-track"><span class="bar-fill" style="width:${Math.min(pct,100)}%"></span></span><span class="t-muted">${pct.toFixed(1)}%</span></span></td>`;
    body.appendChild(tr);});
  if(!show.length)body.innerHTML='<tr><td colspan="10" style="text-align:center;color:var(--text-3);padding:20px">暂无数据</td></tr>';
}

/* ═══════════════ Render All / Events ═══════════════ */
function renderAll(){renderKpi();renderDept();renderUsers();}
function setPeriod(p,btn){period=p;document.querySelectorAll('[data-period]').forEach(b=>b.classList.remove('active'));btn.classList.add('active');renderDept();renderUsers();}

/* ═══════════════ Anchor Nav + Scrollspy ═══════════════ */
function setupAnchors(){
  const anchors=document.querySelectorAll('.anchors .anchor'),secs=['overview','models','dept','users'];
  anchors.forEach(a=>a.addEventListener('click',()=>{const t=document.getElementById('sec-'+a.dataset.sec);if(t)t.scrollIntoView({behavior:'smooth',block:'start'});}));
  const spy=()=>{const off=window.scrollY+120;let cur='overview';for(const s of secs){const el=document.getElementById('sec-'+s);if(el&&el.offsetTop<=off)cur=s;}anchors.forEach(a=>a.classList.toggle('on',a.dataset.sec===cur));};
  window.addEventListener('scroll',spy,{passive:true});
}

/* ═══════════════ Chip Filters ═══════════════ */
function initChips(){
  function cg(cid,opts,gk,gl,ss,oc){const el=document.getElementById(cid);el.innerHTML='';const ac=document.createElement('span');ac.className='chip chip-all'+(ss.size===0?' on':'');ac.textContent='全部';ac.addEventListener('click',()=>{ss.clear();oc();});el.appendChild(ac);for(const o of opts){const k=gk(o),c=document.createElement('span');c.className='chip'+(ss.has(k)?' on':'');c.textContent=gl(o);c.addEventListener('click',()=>{if(ss.has(k))ss.delete(k);else ss.add(k);oc();});el.appendChild(c);}}
  const rf=()=>{initChips();renderAll();};cg('chips-model',MODELS,m=>m.id,m=>m.id,state.models,rf);cg('chips-tier',['L3-Premium','L2-Advanced','L1-Basic','unclassified'],t=>t,t=>TIER_LABEL[t],state.tiers,rf);cg('chips-dept',DEPTS,d=>d,d=>d,state.depts,rf);
  document.getElementById('btn-reset').onclick=()=>{state.models.clear();state.tiers.clear();state.depts.clear();initChips();renderAll();};
  document.getElementById('btn-filter-toggle').addEventListener('click',()=>{const fb=document.querySelector('.filter-bar');fb.classList.toggle('closed');});
}

/* ═══════════════ Init ═══════════════ */
function applyTheme(){document.documentElement.classList.toggle('dark',isDark);document.getElementById('btn-theme').textContent=isDark?'☀':'☾';}
function toggleTheme(){isDark=!isDark;applyTheme();renderAll();}
document.querySelectorAll('[data-period]').forEach(b=>b.addEventListener('click',()=>setPeriod(b.dataset.period,b)));
initChips();
setupAnchors();
renderAll();
applyTheme();
let autoRefresh=true;
document.getElementById('btn-refresh').addEventListener('click',function(){autoRefresh=!autoRefresh;this.innerHTML=autoRefresh?'<span class="dot-live on"></span>自动刷新':'<span class="dot-live"></span>已暂停';});
document.getElementById('btn-theme').addEventListener('click',toggleTheme);
document.getElementById('t-range').addEventListener('change',()=>{document.getElementById('t-compare').textContent=`环比:上一区间`;renderAll();});
setInterval(()=>{if(autoRefresh)renderAll();},30000);
