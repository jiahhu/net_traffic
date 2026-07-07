"use strict";

const state = {
  trafficRange: "day", destinationRange: "day", points: [], daily: [], live: null,
  dailyStart: "", dailyEnd: "", trafficGeometry: null, dailyGeometry: null,
  trafficHover: null, dailyHover: null
};
const $ = (id) => document.getElementById(id);

function formatBytes(value, suffix = "") {
  if (!Number.isFinite(value) || value < 0) return "--";
  const units = ["B", "KB", "MB", "GB", "TB", "PB"];
  let size = value, unit = 0;
  while (size >= 1000 && unit < units.length - 1) { size /= 1000; unit++; }
  const digits = size >= 100 ? 0 : size >= 10 ? 1 : 2;
  return `${size.toFixed(digits)} ${units[unit]}${suffix}`;
}

function formatRate(value) { return formatBytes(value, "/s"); }
function formatExactBytes(value) { return `${formatBytes(value)} (${Math.round(value || 0).toLocaleString("zh-CN")} Bytes)`; }
function formatExactRate(value) { return `${formatRate(value)} (${Math.round(value || 0).toLocaleString("zh-CN")} Bytes/s)`; }
function localDateValue(date) {
  const year = date.getFullYear(), month = String(date.getMonth()+1).padStart(2,"0"), day = String(date.getDate()).padStart(2,"0");
  return `${year}-${month}-${day}`;
}
function formatUptime(seconds) {
  if (!seconds) return "--";
  const days = Math.floor(seconds / 86400), hours = Math.floor((seconds % 86400) / 3600), mins = Math.floor((seconds % 3600) / 60);
  return days ? `${days} 天 ${hours} 小时` : `${hours} 小时 ${mins} 分`;
}
function timeLabel(epoch, range) {
  const d = new Date(epoch * 1000);
  if (range === "day") return d.toLocaleTimeString("zh-CN", {hour: "2-digit", minute: "2-digit", hour12: false});
  return `${d.getMonth()+1}/${d.getDate()}`;
}

async function getJSON(url) {
  const response = await fetch(url, {headers: {Accept: "application/json"}});
  if (!response.ok) {
    const body = await response.json().catch(()=>({}));
    throw new Error(body.error || `请求失败 (${response.status})`);
  }
  return response.json();
}

function showError(error) {
  $("errorBanner").textContent = `数据加载失败：${error.message}`;
  $("errorBanner").hidden = false;
}
function clearError() { $("errorBanner").hidden = true; }

async function loadOverview() {
  const data = await getJSON("/api/overview");
  const s = data.status || {};
  state.live = s;
  updateLive(s);
  $("todayTotal").textContent = formatBytes((data.today.rxBytes || 0) + (data.today.txBytes || 0));
  $("todaySplit").textContent = `入站 ${formatBytes(data.today.rxBytes || 0)} · 出站 ${formatBytes(data.today.txBytes || 0)}`;
  $("monthTotal").textContent = formatBytes((data.month.rxBytes || 0) + (data.month.txBytes || 0));
  $("monthSplit").textContent = `入站 ${formatBytes(data.month.rxBytes || 0)} · 出站 ${formatBytes(data.month.txBytes || 0)}`;
  $("version").textContent = data.version || "dev";
  $("destinationWarning").hidden = s.destinationTracking !== false;
}

function updateLive(s) {
  if (!s) return;
  $("interfaceName").textContent = s.interface || "--";
  $("rxRate").textContent = formatRate(s.rxRate || 0);
  $("txRate").textContent = formatRate(s.txRate || 0);
  $("rxCurrent").textContent = formatRate(s.rxRate || 0);
  $("txCurrent").textContent = formatRate(s.txRate || 0);
  $("uptime").textContent = formatUptime(s.uptime);
  $("updatedAt").textContent = s.time ? new Date(s.time * 1000).toLocaleString("zh-CN", {hour12: false}) : "--";
}

async function loadTraffic() {
  const data = await getJSON(`/api/series?range=${state.trafficRange}`);
  state.points = data.points || [];
  const names = {day: "每日 图表 (5 分钟 平均)", week: "每周 图表 (30 分钟 平均)", month: "每月 图表 (2 小时 平均)"};
  $("trafficTitle").textContent = names[state.trafficRange];
  renderTraffic();
}

function renderTraffic() {
  const points = state.points;
  $("trafficEmpty").hidden = points.length > 1;
  state.trafficGeometry = drawLineChart($("trafficChart"), points, state.trafficRange, state.trafficHover);
  updateStats(points);
}

function updateStats(points) {
  for (const key of ["rx", "tx"]) {
    const values = points.map(p => key === "rx" ? p.rxRate : p.txRate);
    const current = state.live ? (key === "rx" ? state.live.rxRate : state.live.txRate) : (values.at(-1) || 0);
    const max = values.length ? Math.max(...values) : 0;
    const avg = values.length ? values.reduce((a,b) => a+b, 0) / values.length : 0;
    $(`${key}Max`).textContent = formatRate(max);
    $(`${key}Avg`).textContent = formatRate(avg);
    $(`${key}Current`).textContent = formatRate(current || 0);
  }
}

function canvasContext(canvas) {
  const ratio = Math.max(1, window.devicePixelRatio || 1);
  const rect = canvas.getBoundingClientRect();
  canvas.width = Math.floor(rect.width * ratio);
  canvas.height = Math.floor(rect.height * ratio);
  const ctx = canvas.getContext("2d");
  ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  return {ctx, width: rect.width, height: rect.height};
}

function niceMax(value) {
  if (value <= 0) return 1000;
  const power = 10 ** Math.floor(Math.log10(value));
  return Math.ceil(value / power * 1.12) * power;
}

function drawGrid(ctx, width, height, margin, maxY, labels, xLabel, verticals = 12, horizontals = 10) {
  const plotW = width - margin.left - margin.right, plotH = height - margin.top - margin.bottom;
  ctx.clearRect(0, 0, width, height);
  ctx.fillStyle = "#eeeeee"; ctx.fillRect(margin.left, margin.top, plotW, plotH);
  ctx.strokeStyle = "#555"; ctx.lineWidth = 1; ctx.setLineDash([1, 2]);
  ctx.font = "9px Arial"; ctx.fillStyle = "#000"; ctx.textAlign = "right"; ctx.textBaseline = "middle";
  for (let i=0; i<=horizontals; i++) {
    const y = margin.top + plotH * i / horizontals;
    ctx.beginPath(); ctx.moveTo(margin.left, y); ctx.lineTo(width-margin.right, y); ctx.stroke();
    if (i % 2 === 0) ctx.fillText(formatBytes(maxY * (horizontals-i) / horizontals), margin.left-7, y);
  }
  ctx.textAlign = "center"; ctx.textBaseline = "top";
  const labelEvery = Math.max(1, Math.ceil(verticals / 6));
  for (let i=0; i<=verticals; i++) {
    const x = margin.left + plotW * i / verticals;
    ctx.beginPath(); ctx.moveTo(x, margin.top); ctx.lineTo(x, height-margin.bottom); ctx.stroke();
    if (i % labelEvery === 0 || i === verticals) ctx.fillText(xLabel(i/verticals), x, height-margin.bottom+7);
  }
  ctx.setLineDash([]); ctx.strokeStyle = "#000"; ctx.strokeRect(margin.left, margin.top, plotW, plotH);
  ctx.save();
  ctx.translate(9, margin.top + plotH/2);
  ctx.rotate(-Math.PI/2);
  ctx.textAlign = "center"; ctx.textBaseline = "top"; ctx.font = "9px Arial"; ctx.fillStyle = "#000";
  ctx.fillText(labels === "rate" ? "Bytes per second" : "Bytes per day", 0, 0);
  ctx.restore();
}

function drawLineChart(canvas, points, range, hoverIndex = null) {
  const {ctx, width, height} = canvasContext(canvas), margin = {left: 78, right: 14, top: 12, bottom: 28};
  const maxY = niceMax(Math.max(0, ...points.flatMap(p => [p.rxRate || 0, p.txRate || 0])));
  const start = points[0]?.time || Date.now()/1000-86400, end = points.at(-1)?.time || Date.now()/1000;
  const verticals = range === "day" ? 24 : range === "week" ? 14 : 15;
  drawGrid(ctx, width, height, margin, maxY, "rate", t => timeLabel(start+(end-start)*t, range), verticals, 10);
  const geometry = {margin, width, height, plotW: width-margin.left-margin.right, plotH: height-margin.top-margin.bottom, start, end, maxY};
  if (points.length < 2) return geometry;
  const plotW = width-margin.left-margin.right, plotH=height-margin.top-margin.bottom;
  const draw = (key, color, width) => {
    ctx.beginPath();
    points.forEach((p, i) => { const x=margin.left+((p.time-start)/(end-start||1))*plotW, y=margin.top+plotH-((p[key]||0)/maxY)*plotH; i?ctx.lineTo(x,y):ctx.moveTo(x,y); });
    ctx.strokeStyle=color; ctx.lineWidth=width; ctx.stroke();
  };
  draw("rxRate", "#00aa38", 2); draw("txRate", "#0000aa", 1);
  if (Number.isInteger(hoverIndex) && points[hoverIndex]) {
    const p=points[hoverIndex], x=margin.left+((p.time-start)/(end-start||1))*plotW;
    ctx.strokeStyle="#aa0000";ctx.lineWidth=1;ctx.setLineDash([3,2]);ctx.beginPath();ctx.moveTo(x,margin.top);ctx.lineTo(x,margin.top+plotH);ctx.stroke();ctx.setLineDash([]);
    for (const [key,color] of [["rxRate","#00aa38"],["txRate","#0000aa"]]) { const y=margin.top+plotH-((p[key]||0)/maxY)*plotH;ctx.fillStyle=color;ctx.beginPath();ctx.arc(x,y,3,0,Math.PI*2);ctx.fill(); }
  }
  return geometry;
}

async function loadDaily() {
  const query = new URLSearchParams({start: state.dailyStart, end: state.dailyEnd});
  const data = await getJSON(`/api/daily?${query}`);
  state.daily = data.days || [];
  state.dailyHover = null;
  $("dailyTooltip").hidden = true;
  $("dailyEmpty").hidden = state.daily.length > 0;
  drawDailyChart();
}

function drawDailyChart() {
  const canvas = $("dailyChart"), rows = state.daily;
  const {ctx,width,height}=canvasContext(canvas), margin={left:78,right:14,top:12,bottom:30};
  const maxY=niceMax(Math.max(0,...rows.flatMap(d=>[d.rxBytes||0,d.txBytes||0])));
  drawGrid(ctx,width,height,margin,maxY,"bytes",t=>{ if(!rows.length)return ""; const i=Math.min(rows.length-1,Math.floor(t*(rows.length-1))); return rows[i].date.slice(5); },15,10);
  const geometry={margin,width,height,plotW:width-margin.left-margin.right,plotH:height-margin.top-margin.bottom};
  if (!rows.length) { state.dailyGeometry=geometry; return; }
  const plotW=width-margin.left-margin.right, plotH=height-margin.top-margin.bottom, group=plotW/rows.length, bar=Math.max(2,Math.min(12,group*.3));
  rows.forEach((d,i)=>{ const x=margin.left+i*group+group/2; const rx=(d.rxBytes/maxY)*plotH, tx=(d.txBytes/maxY)*plotH; ctx.fillStyle="#00aa38";ctx.fillRect(x-bar-1,margin.top+plotH-rx,bar,rx);ctx.fillStyle="#0000aa";ctx.fillRect(x+1,margin.top+plotH-tx,bar,tx); });
  if (Number.isInteger(state.dailyHover) && rows[state.dailyHover]) {
    const x=margin.left+state.dailyHover*group+group/2;
    ctx.strokeStyle="#aa0000";ctx.lineWidth=1;ctx.setLineDash([3,2]);ctx.beginPath();ctx.moveTo(x,margin.top);ctx.lineTo(x,margin.top+plotH);ctx.stroke();ctx.setLineDash([]);
  }
  state.dailyGeometry={...geometry,group};
}

async function loadDestinations() {
  const data = await getJSON(`/api/destinations?range=${state.destinationRange}`);
  renderDestinations(data.destinations || []);
}

function renderDestinations(rows) {
  const list=$("destinationList"); list.replaceChildren(); $("destinationEmpty").hidden=rows.length>0;
  const max=rows[0]?.bytes || 1;
  rows.forEach(row=>{ const li=document.createElement("li"), main=document.createElement("div"), name=document.createElement("span"), ip=document.createElement("span"), bar=document.createElement("span"), fill=document.createElement("i"), value=document.createElement("span"); main.className="destination-main";name.className="destination-name";ip.className="destination-ip";bar.className="destination-bar";value.className="destination-value";name.textContent=row.host;ip.textContent=row.ip;fill.style.width=`${Math.max(2,row.bytes/max*100)}%`;value.textContent=formatBytes(row.bytes);bar.append(fill);main.append(name,ip,bar);li.append(main,value);list.append(li); });
}

function setupRanges() {
  $("trafficRange").addEventListener("click", async e=>{ const button=e.target.closest("button[data-range]"); if(!button)return; state.trafficRange=button.dataset.range; setActive($("trafficRange"),button); try{await loadTraffic();}catch(err){showError(err);} });
  $("destinationRange").addEventListener("click", async e=>{ const button=e.target.closest("button[data-range]"); if(!button)return; state.destinationRange=button.dataset.range; setActive($("destinationRange"),button); try{await loadDestinations();}catch(err){showError(err);} });
}
function setActive(group, selected) { group.querySelectorAll("button").forEach(b=>b.classList.toggle("active",b===selected)); }

function setupDailyRange() {
  const end=new Date(), start=new Date(); start.setDate(end.getDate()-29);
  state.dailyStart=localDateValue(start); state.dailyEnd=localDateValue(end);
  $("dailyStart").value=state.dailyStart; $("dailyEnd").value=state.dailyEnd;
  $("dailyStart").max=state.dailyEnd; $("dailyEnd").max=state.dailyEnd;
  $("dailyRangeForm").addEventListener("submit", async event=>{
    event.preventDefault();
    const startValue=$("dailyStart").value, endValue=$("dailyEnd").value;
    if (!startValue || !endValue || startValue>endValue) { showError(new Error("开始日期不能晚于结束日期")); return; }
    const span=(new Date(`${endValue}T00:00:00`)-new Date(`${startValue}T00:00:00`))/86400000;
    if (span>366) { showError(new Error("自定义时间范围不能超过 366 天")); return; }
    state.dailyStart=startValue; state.dailyEnd=endValue;
    try { await loadDaily(); clearError(); } catch(err) { showError(err); }
  });
}

function positionTooltip(tooltip, canvas, x, y) {
  tooltip.hidden=false;
  const width=canvas.getBoundingClientRect().width, height=canvas.getBoundingClientRect().height;
  const left=Math.max(3,Math.min(width-tooltip.offsetWidth-3,x+12));
  const top=Math.max(3,Math.min(height-tooltip.offsetHeight-3,y+12));
  tooltip.style.left=`${left}px`; tooltip.style.top=`${top}px`;
}

function setupTooltips() {
  const traffic=$("trafficChart"), trafficTip=$("trafficTooltip");
  traffic.addEventListener("pointermove", event=>{
    const g=state.trafficGeometry;
    if(!g || state.points.length<2)return;
    const rect=traffic.getBoundingClientRect(), x=event.clientX-rect.left, y=event.clientY-rect.top;
    if(x<g.margin.left || x>g.margin.left+g.plotW || y<g.margin.top || y>g.margin.top+g.plotH){trafficTip.hidden=true;return;}
    const target=g.start+((x-g.margin.left)/g.plotW)*(g.end-g.start);
    let index=0, distance=Infinity;
    state.points.forEach((p,i)=>{const d=Math.abs(p.time-target);if(d<distance){distance=d;index=i;}});
    state.trafficHover=index; renderTraffic();
    const p=state.points[index], date=new Date(p.time*1000).toLocaleString("zh-CN",{hour12:false});
    trafficTip.textContent=`${date}\nINPUT: ${formatExactRate(p.rxRate)}\nOUTPUT: ${formatExactRate(p.txRate)}`;
    positionTooltip(trafficTip,traffic,x,y);
  });
  traffic.addEventListener("pointerleave",()=>{state.trafficHover=null;trafficTip.hidden=true;renderTraffic();});

  const daily=$("dailyChart"), dailyTip=$("dailyTooltip");
  daily.addEventListener("pointermove",event=>{
    const g=state.dailyGeometry;
    if(!g || !g.group || !state.daily.length)return;
    const rect=daily.getBoundingClientRect(), x=event.clientX-rect.left, y=event.clientY-rect.top;
    if(x<g.margin.left || x>g.margin.left+g.plotW || y<g.margin.top || y>g.margin.top+g.plotH){dailyTip.hidden=true;return;}
    const index=Math.max(0,Math.min(state.daily.length-1,Math.floor((x-g.margin.left)/g.group)));
    state.dailyHover=index; drawDailyChart();
    const d=state.daily[index];
    dailyTip.textContent=`${d.date}\nINPUT: ${formatExactBytes(d.rxBytes)}\nOUTPUT: ${formatExactBytes(d.txBytes)}\n合计: ${formatExactBytes(d.rxBytes+d.txBytes)}`;
    positionTooltip(dailyTip,daily,x,y);
  });
  daily.addEventListener("pointerleave",()=>{state.dailyHover=null;dailyTip.hidden=true;drawDailyChart();});
}

function connectLive() {
  const stream=new EventSource("/api/live");
  stream.addEventListener("status", e=>{ const status=JSON.parse(e.data); state.live=status; updateLive(status); $("connectionState").textContent="实时在线"; clearError(); if(state.trafficRange==="day"){ state.points.push({time:status.time,rxRate:status.rxRate,txRate:status.txRate}); const cutoff=status.time-86400; state.points=state.points.filter(p=>p.time>=cutoff); renderTraffic(); } });
  stream.onerror=()=>{ $("connectionState").textContent="正在重连"; };
}

let resizeTimer;
window.addEventListener("resize",()=>{ clearTimeout(resizeTimer);resizeTimer=setTimeout(()=>{renderTraffic();drawDailyChart();},120); });

async function init() {
  setupDailyRange();
  setupRanges();
  setupTooltips();
  try { await Promise.all([loadOverview(),loadTraffic(),loadDaily(),loadDestinations()]); clearError(); } catch(err) { showError(err); }
  connectLive();
  setInterval(()=>loadOverview().catch(showError),60000);
  setInterval(()=>loadDestinations().catch(showError),15000);
  setInterval(()=>loadDaily().catch(showError),300000);
}
init();
