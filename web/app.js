const USER_LIST = [
  { key: "mw071175maj", name: "Oleksii" },
  { key: "mw301188lvi", name: "Vitalka" },
  { key: "mw300389kiy", name: "Иван К" },
  { key: "mw190586kji", name: "Юля К" },
  { key: "mw101094amb", name: "Марина А" },
  { key: "mw261092mes", name: "Елена М" },
];

const runBtn = document.getElementById("run");
const statusEl = document.getElementById("status");
const outputEl = document.getElementById("output");
const projectsBox = document.getElementById("projectsBox");
const usersBox = document.getElementById("usersBox");
const previewBtn = document.getElementById("preview");
const analysisFlag = document.getElementById("analysis");
const showRawFlag = document.getElementById("showRaw");
const queryInput = document.getElementById("query");
const jqlInput = document.getElementById("jql");
let allProjects = [];
const phrasesList = document.getElementById("phrasesList");
const phraseInput = document.getElementById("phraseInput");
const phraseSaveBtn = document.getElementById("phraseSave");
const phraseCancelBtn = document.getElementById("phraseCancel");
let phrases = [];
let editIndex = null;
let selectedPhrase = null;
const sprintsBox = document.getElementById("sprintsBox");
let projectSprints = [];
let selectedSprintId = 0;
let currentProjectKey = null;
const stepsPanel = document.getElementById("stepsPanel");
const historyListEl = document.getElementById("historyList");
const historyDetailEl = document.getElementById("historyDetail");
const commandInput = document.getElementById("commandInput");
const commandRunBtn = document.getElementById("commandRun");
const commandOutput = document.getElementById("commandOutput");
let historyEntries = [];
let currentHistoryId = null;

// Если пользователь меняет текст запроса — сбрасываем JQL, чтобы не прилипало старое.
queryInput.addEventListener("input", () => {
  jqlInput.value = "";
});

function buildPayload(dryRun = false) {
  return {
    query: getQueryValue(),
    jql: document.getElementById("jql").value,
    sprintId: selectedSprintId || 0,
    projects: getCheckedValues("projectsBox", allProjects),
    users: getCheckedValues("usersBox", USER_LIST.map((u) => u.key)),
    dryRun,
    analysis: analysisFlag.checked,
  };
}

previewBtn.addEventListener("click", async () => {
  await runSearch(true);
});

runBtn.addEventListener("click", async () => {
  await runSearch(false);
});

async function runSearch(dryRun) {
  statusEl.textContent = dryRun ? "Previewing..." : "Running...";
  outputEl.textContent = "";
  const payload = buildPayload(dryRun);

  try {
    const res = await fetch("/api/search", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!res.ok) {
      throw new Error(data.error || res.statusText);
    }
    document.getElementById("jql").value = data.jql; // show final JQL
    const rawText =
      data.raw && showRawFlag.checked
        ? typeof data.raw === "string"
          ? JSON.stringify(JSON.parse(data.raw), null, 2)
          : JSON.stringify(data.raw, null, 2)
        : "";
    statusEl.textContent = dryRun ? `Preview JQL ready` : `OK, executed`;
    const analysisBlock = data.analysis ? `Analysis:\n${data.analysis}\n\n` : "";
    const linksBlock = buildIssuesList(data.raw, data.issues);
    const totalBlock = data.total ? `Total: ${data.total}\n\n` : "";
    const rawBlock = rawText ? `Raw:\n${rawText}` : "";
    outputEl.textContent = `JQL: ${data.jql}\n\n${totalBlock}${analysisBlock}${linksBlock}${rawBlock}`;
    renderSteps(data.steps || []);
    if (!dryRun) {
      if (data.historyId) {
        setCurrentHistoryId(data.historyId);
        await loadHistoryEntry(data.historyId, { focusOutput: false });
      }
      await loadHistoryEntries();
    }
  } catch (err) {
    statusEl.textContent = `Error: ${err.message}`;
  }
}

async function loadMyself() {
  try {
    const res = await fetch("/api/myself");
    const data = await res.json();
    if (res.ok) {
      statusEl.textContent = `Hello, ${data.displayName}`;
    } else {
      statusEl.textContent = `Auth error: ${data.error || res.statusText}`;
    }
  } catch (err) {
    statusEl.textContent = `Auth error: ${err.message}`;
  }
}

function getCheckedValues(containerId, allValues = []) {
  const box = document.getElementById(containerId);
  if (!box) return [];
  const firstInput = box.querySelector("input");
  if (!firstInput) return [];
  if (firstInput.type === "radio") {
    const checked = box.querySelector('input[type="radio"]:checked');
    if (!checked) return [];
    if (checked.dataset.role === "all") return allValues;
    return [checked.value];
  }
  const all = box.querySelector('input[data-role="all"]');
  if (all && all.checked) return allValues;
  return Array.from(box.querySelectorAll('input[type="checkbox"]:not([data-role="all"])'))
    .filter((c) => c.checked)
    .map((c) => c.value);
}

function setupAllToggle(containerId, singleSelect = false) {
  const box = document.getElementById(containerId);
  if (!box) return;
  const all = box.querySelector('input[data-role="all"]');
  if (!all) return;
  all.addEventListener("change", () => {
    if (all.checked) {
      box.querySelectorAll('input[type="checkbox"]:not([data-role="all"])').forEach((c) => {
        c.checked = false;
      });
    }
  });
  const items = box.querySelectorAll('input[type="checkbox"]:not([data-role="all"])');
  items.forEach((c) => {
    c.addEventListener("change", () => {
      if (c.checked) {
        all.checked = false;
        if (singleSelect) {
          items.forEach((other) => {
            if (other !== c) other.checked = false;
          });
        }
      } else {
        const anyChecked = Array.from(items).some((v) => v.checked);
        if (!anyChecked) {
          all.checked = true;
        }
      }
    });
  });
}

async function loadProjects() {
  try {
    const res = await fetch("/api/projects");
    if (!res.ok) return;
    const data = await res.json();
    allProjects = data.map((p) => p.key);
    projectsBox.innerHTML = "";
    const all = document.createElement("label");
    all.innerHTML = `<input type="radio" name="projectsRadio" data-role="all" checked> Все проекты`;
    projectsBox.appendChild(all);
    data.forEach((p) => {
      const label = document.createElement("label");
      label.innerHTML = `<input type="radio" name="projectsRadio" value="${p.key}"> ${p.key} — ${p.name}`;
      projectsBox.appendChild(label);
    });
    projectsBox.addEventListener("change", handleProjectChange);
  } catch (err) {
    console.error("loadProjects", err);
  }
}

function renderUsers() {
  const users = USER_LIST;
  usersBox.innerHTML = "";
  const all = document.createElement("label");
  all.innerHTML = `<input type="checkbox" data-role="all" checked> Все пользователи`;
  usersBox.appendChild(all);
  users.forEach((u) => {
    const label = document.createElement("label");
    label.innerHTML = `<input type="checkbox" value="${u.key}"> ${u.name} (${u.key})`;
    usersBox.appendChild(label);
  });
  setupAllToggle("usersBox");
}

// initial call
loadMyself();
renderUsers();
loadProjects();
loadPhrases().then(renderPhrases);
loadHistoryEntries();
if (commandRunBtn) {
  commandRunBtn.addEventListener("click", executeCommand);
}

function buildIssuesList(raw, issuesFromResponse) {
  if (issuesFromResponse && issuesFromResponse.length) {
    const lines = issuesFromResponse.map((i) => `${i.key}: ${i.title} - ${i.url}`);
    return `Issues:\n${lines.join("\n")}\n\n`;
  }
  if (!raw) return "";
  let data;
  try {
    data = typeof raw === "string" ? JSON.parse(raw) : raw;
  } catch {
    return "";
  }
  if (!data.issues || !Array.isArray(data.issues) || data.issues.length === 0) {
    return "";
  }
  const lines = data.issues.map((iss) => {
    const key = iss.key || (iss.fields && iss.fields.key) || "";
    const summary = iss.fields && iss.fields.summary ? iss.fields.summary : "";
    return `${key}: ${summary} - https://jira.corezoid.com/browse/${key}`;
  });
  return `Issues:\n${lines.join("\n")}\n\n`;
}

async function handleProjectChange() {
  const selected = getCheckedValues("projectsBox", allProjects);
  selectedSprintId = 0;
  renderSprints([]);
  if (!selected.length || selected[0] === "all") {
    currentProjectKey = null;
    return;
  }
  currentProjectKey = selected[0];
  try {
    const res = await fetch(`/api/projects/${selected[0]}/sprints?limit=5`);
    if (!res.ok) {
      renderSprints([], true);
      return;
    }
    const data = await res.json();
    if (!data || !Array.isArray(data)) {
      renderSprints([], true);
      return;
    }
    projectSprints = data;
    renderSprints(projectSprints, true);
  } catch (err) {
    console.error("load sprints", err);
    renderSprints([], true);
  }
}

function renderSprints(list, hasProject = false) {
  sprintsBox.innerHTML = "";
  const all = document.createElement("label");
  all.innerHTML = `<input type="radio" name="sprintsRadio" data-role="all" checked> Весь проект`;
  sprintsBox.appendChild(all);
  if (!list || list.length === 0) {
    const msg = document.createElement("div");
    msg.style.fontSize = "12px";
    msg.style.color = "#555";
    msg.textContent = hasProject ? "Спринтов не найдено" : "Выберите проект, чтобы увидеть спринты";
    sprintsBox.appendChild(msg);
  } else {
    list.forEach((sp) => {
      const label = document.createElement("label");
      const start = sp.startDate ? sp.startDate.slice(0, 10) : "";
      const end = sp.endDate ? sp.endDate.slice(0, 10) : "";
      label.innerHTML = `<input type="radio" name="sprintsRadio" value="${sp.id}"> ${sp.name} (${start} - ${end})`;
      label.querySelector("input").addEventListener("change", () => {
        selectedSprintId = Number(sp.id);
      });
      sprintsBox.appendChild(label);
    });
  }
  const radios = sprintsBox.querySelectorAll('input[type="radio"]');
  radios.forEach((r) => {
    r.addEventListener("change", () => {
      const checked = sprintsBox.querySelector('input[type="radio"]:checked');
      if (checked && checked.dataset.role === "all") {
        selectedSprintId = 0;
      } else if (checked) {
        selectedSprintId = Number(checked.value);
      }
    });
  });
}

function getQueryValue() {
  return selectedPhrase || queryInput.value;
}

async function loadPhrases() {
  try {
    const res = await fetch("/api/phrases");
    if (!res.ok) {
      phrases = [];
      return;
    }
    const data = await res.json();
    phrases = Array.isArray(data) ? data : [];
  } catch {
    phrases = [];
  }
}

async function savePhrases() {
  try {
    await fetch("/api/phrases", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ phrases }),
    });
  } catch (err) {
    console.error("savePhrases", err);
  }
}

function renderPhrases() {
  phrasesList.innerHTML = "";
  phrases.forEach((p, idx) => {
    const li = document.createElement("li");
    const text = document.createElement("span");
    text.className = "text";
    text.textContent = p;
    const actions = document.createElement("div");
    actions.className = "actions";
    const selectBtn = document.createElement("button");
    selectBtn.textContent = selectedPhrase === p ? "Снять выбор" : "Выбрать";
    selectBtn.addEventListener("click", () => {
      if (selectedPhrase === p) {
        selectedPhrase = null;
        queryInput.disabled = false;
        jqlInput.value = "";
      } else {
        selectedPhrase = p;
        queryInput.value = p;
        queryInput.disabled = true;
        jqlInput.value = "";
      }
      renderPhrases();
    });
    const editBtn = document.createElement("button");
    editBtn.textContent = "Редакт.";
    editBtn.addEventListener("click", () => {
      phraseInput.value = p;
      editIndex = idx;
    });
    const delBtn = document.createElement("button");
    delBtn.textContent = "Удалить";
    delBtn.addEventListener("click", async () => {
      phrases.splice(idx, 1);
      if (selectedPhrase === p) {
        selectedPhrase = null;
        queryInput.disabled = false;
      }
      await savePhrases();
      renderPhrases();
    });
    actions.appendChild(selectBtn);
    actions.appendChild(editBtn);
    actions.appendChild(delBtn);
    li.appendChild(text);
    li.appendChild(actions);
    phrasesList.appendChild(li);
  });
  if (!selectedPhrase) {
    queryInput.disabled = false;
  }
}

phraseSaveBtn.addEventListener("click", async () => {
  const val = phraseInput.value.trim();
  if (!val) return;
  if (editIndex !== null) {
    phrases[editIndex] = val;
    editIndex = null;
  } else {
    phrases.push(val);
  }
  phraseInput.value = "";
  await savePhrases();
  renderPhrases();
});

phraseCancelBtn.addEventListener("click", () => {
  selectedPhrase = null;
  queryInput.disabled = false;
  editIndex = null;
  phraseInput.value = "";
  jqlInput.value = "";
  renderPhrases();
});

async function loadHistoryEntries() {
  if (!historyListEl) return;
  try {
    const res = await fetch("/api/history");
    if (!res.ok) {
      historyListEl.textContent = "Не удалось загрузить историю";
      return;
    }
    const data = await res.json();
    historyEntries = Array.isArray(data) ? data : [];
    renderHistoryList(historyEntries);
  } catch (err) {
    historyListEl.textContent = "Не удалось загрузить историю";
    console.error("loadHistoryEntries", err);
  }
}

function renderHistoryList(entries) {
  if (!historyListEl) return;
  historyListEl.innerHTML = "";
  if (!entries.length) {
    historyListEl.textContent = "История пустая";
    return;
  }
  entries.forEach((entry) => {
    const item = document.createElement("div");
    item.className = "history-item";
    const text = document.createElement("div");
    text.innerHTML = `<strong>${entry.query || "Без запроса"}</strong><br/><small>${formatDate(entry.createdAt)}</small>`;
    const btn = document.createElement("button");
    btn.textContent = "Открыть";
    btn.addEventListener("click", async () => {
      await loadHistoryEntry(entry.id);
    });
    item.appendChild(text);
    item.appendChild(btn);
    historyListEl.appendChild(item);
  });
}

async function loadHistoryEntry(entryId, options = {}) {
  if (!historyDetailEl) return;
  try {
    const res = await fetch(`/api/history/${entryId}`);
    if (!res.ok) {
      historyDetailEl.textContent = `Ошибка ${res.status}`;
      return;
    }
    const entry = await res.json();
    renderHistoryEntryDetail(entry);
    if (options.focusOutput !== false) {
      populateOutputFromEntry(entry);
    }
    setCurrentHistoryId(entry.id);
    if (commandOutput) {
      commandOutput.innerHTML = "";
    }
  } catch (err) {
    historyDetailEl.textContent = "Не удалось загрузить запись";
    console.error("loadHistoryEntry", err);
  }
}

function renderHistoryEntryDetail(entry) {
  if (!historyDetailEl) return;
  historyDetailEl.innerHTML = "";
  if (!entry) {
    historyDetailEl.textContent = "Выберите ответ из списка";
    return;
  }
  const title = document.createElement("h3");
  title.textContent = entry.query || "Без запроса";
  const meta = document.createElement("div");
  meta.className = "detail-row";
  meta.textContent = `JQL: ${entry.jql}`;
  const issuesRow = document.createElement("div");
  issuesRow.className = "detail-row";
  issuesRow.textContent = `Задач: ${entry.issues?.length || 0}`;
  const actions = document.createElement("div");
  actions.className = "history-actions";
  const openBtn = document.createElement("button");
  openBtn.textContent = "Показать в результатах";
  openBtn.addEventListener("click", () => populateOutputFromEntry(entry));
  const searchInput = document.createElement("input");
  searchInput.type = "text";
  searchInput.placeholder = "Искать в задачах истории";
  const searchBtn = document.createElement("button");
  searchBtn.textContent = "Найти";
  const matchesEl = document.createElement("div");
  matchesEl.id = "historyMatches";
  matchesEl.className = "detail-row";
  searchBtn.addEventListener("click", async () => {
    const query = searchInput.value.trim();
    if (!query) return;
    await searchHistoryMatches(entry.id, query);
  });
  actions.appendChild(openBtn);
  actions.appendChild(searchInput);
  actions.appendChild(searchBtn);
  historyDetailEl.appendChild(title);
  historyDetailEl.appendChild(meta);
  historyDetailEl.appendChild(issuesRow);
  historyDetailEl.appendChild(actions);
  historyDetailEl.appendChild(matchesEl);
  if (entry.steps && entry.steps.length) {
    const stepsList = document.createElement("div");
    stepsList.className = "detail-row";
    stepsList.textContent = `Шагов: ${entry.steps.length}`;
    historyDetailEl.appendChild(stepsList);
  }
}

async function searchHistoryMatches(entryId, query) {
  if (!historyDetailEl) return;
  try {
    const res = await fetch(`/api/history/${entryId}/search`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query }),
    });
    if (!res.ok) {
      throw new Error(`${res.status}`);
    }
    const data = await res.json();
    renderHistoryMatches(data.matches || []);
  } catch (err) {
    const matchesEl = historyDetailEl.querySelector("#historyMatches");
    if (matchesEl) {
      matchesEl.textContent = "Ошибка поиска";
    }
    console.error("searchHistoryMatches", err);
  }
}

function renderHistoryMatches(matches) {
  const matchesEl = historyDetailEl?.querySelector("#historyMatches");
  if (!matchesEl) return;
  if (!matches.length) {
    matchesEl.textContent = "Совпадений не найдено.";
    return;
  }
  matchesEl.innerHTML = "";
  const list = document.createElement("ul");
  list.className = "phrases-list";
  matches.forEach((item) => {
    const li = document.createElement("li");
    li.textContent = `${item.key}: ${item.title}`;
    list.appendChild(li);
  });
  matchesEl.appendChild(list);
}

function populateOutputFromEntry(entry) {
  if (!entry) return;
  statusEl.textContent = `Загружена история — ${entry.query || "без запроса"}`;
  const analysisBlock = entry.analysis ? `Analysis:\n${entry.analysis}\n\n` : "";
  const issuesBlock = formatIssuesList(entry.issues);
  outputEl.textContent = `JQL: ${entry.jql}\n\n${analysisBlock}${issuesBlock}`;
  renderSteps(entry.steps || []);
  setCurrentHistoryId(entry.id);
}

function formatIssuesList(issues) {
  if (!issues || !issues.length) return "";
  const lines = issues.map((iss) => `${iss.key}: ${iss.title} - ${iss.url}`);
  return `Issues:\n${lines.join("\n")}\n\n`;
}

function renderSteps(steps) {
  if (!stepsPanel) return;
  stepsPanel.innerHTML = "";
  if (!steps || !steps.length) {
    stepsPanel.innerHTML = `<div class="step-card">Шаги будут показаны здесь после выполнения запроса.</div>`;
    return;
  }
  steps.forEach((step) => {
    const card = document.createElement("div");
    card.className = "step-card";
    const header = document.createElement("div");
    header.className = "step-name";
    const stepName = document.createElement("span");
    stepName.textContent = step.name;
    const status = document.createElement("span");
    status.className = "step-status";
    status.textContent = step.status || "pending";
    header.appendChild(stepName);
    header.appendChild(status);
    card.appendChild(header);
    if (step.description) {
      const desc = document.createElement("div");
      desc.textContent = step.description;
      card.appendChild(desc);
    }
    const resultText = step.result ? formatStepResult(step.result) : "";
    if (resultText) {
      const pre = document.createElement("pre");
      pre.textContent = resultText;
      card.appendChild(pre);
    }
    stepsPanel.appendChild(card);
  });
}

function formatStepResult(raw) {
  try {
    const parsed = typeof raw === "string" ? JSON.parse(raw) : raw;
    return JSON.stringify(parsed, null, 2);
  } catch {
    return typeof raw === "string" ? raw : "";
  }
}

function formatDate(value) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function setCurrentHistoryId(id) {
  currentHistoryId = id || null;
}

async function executeCommand() {
  if (!commandInput || !commandRunBtn || !commandOutput) return;
  const command = commandInput.value.trim();
  if (!command) return;
  if (!currentHistoryId) {
    appendCommandEntry("Нет истории для команды.");
    return;
  }
  commandRunBtn.disabled = true;
  commandInput.disabled = true;
  appendCommandEntry(`> ${command}`);
  try {
    const res = await fetch(`/api/history/${currentHistoryId}/action`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ command }),
    });
    if (!res.ok) {
      const data = await res.json().catch(() => ({}));
      const message = data.error || res.statusText;
      appendCommandEntry(`Ошибка: ${message}`);
      return;
    }
    const data = await res.json();
    appendCommandEntry(data.result || "Пустой ответ.");
  } catch (err) {
    appendCommandEntry(`Ошибка выполнения: ${err.message}`);
  } finally {
    commandRunBtn.disabled = false;
    commandInput.disabled = false;
  }
}

function appendCommandEntry(text) {
  if (!commandOutput) return;
  const row = document.createElement("div");
  row.className = "command-entry";
  row.textContent = text;
  commandOutput.appendChild(row);
  commandOutput.scrollTop = commandOutput.scrollHeight;
}

