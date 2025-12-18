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

