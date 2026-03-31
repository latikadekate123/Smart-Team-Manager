import { useEffect, useMemo, useRef, useState } from "react";
import * as Y from "yjs";

const API_BASE = import.meta.env.VITE_API_BASE || "";
const WS_BASE =
  import.meta.env.VITE_WS_BASE ||
  `${window.location.protocol === "https:" ? "wss" : "ws"}://${window.location.host}`;

const emptyTask = {
  id: "",
  title: "",
  description: "",
  subtasks: [],
  status: "todo",
  assignee: "",
  priority: "medium",
  estimateHours: 0,
  dueDate: null,
};

const emptyMetrics = {
  total: 0,
  overdue: 0,
  dueSoon: 0,
  statusCounts: { todo: 0, "in-progress": 0, blocked: 0, done: 0 },
  workloadByAssignee: {},
};

export default function App() {
  const [phase, setPhase] = useState("auth");
  const [authMode, setAuthMode] = useState("login");
  const [loginRole, setLoginRole] = useState("member");
  const [profile, setProfile] = useState({
    name: "Latika Dekate",
    email: "latikadekate16@gmail.com",
    password: "mynewproject",
    position: "Project Lead",
  });
  const [user, setUser] = useState(null);
  const [teams, setTeams] = useState([]);
  const [teamMembersMap, setTeamMembersMap] = useState({});
  const [ownerCodes, setOwnerCodes] = useState({});
  const [invitations, setInvitations] = useState([]);
  const [pendingTeam, setPendingTeam] = useState(null);
  const [teamCode, setTeamCode] = useState("");
  const [activeTeam, setActiveTeam] = useState(null);
  const [createTeamForm, setCreateTeamForm] = useState({ name: "", description: "", status: "ongoing" });
  const [inviteRows, setInviteRows] = useState([{ email: "", position: "" }]);
  const [createdTeam, setCreatedTeam] = useState(null);

  const [tasks, setTasks] = useState([]);
  const [metrics, setMetrics] = useState(emptyMetrics);
  const [selectedId, setSelectedId] = useState("");
  const [syncState, setSyncState] = useState("idle");
  const [isGenerating, setIsGenerating] = useState(false);
  const [now, setNow] = useState(new Date());
  const [message, setMessage] = useState("");
  const [filterStatus, setFilterStatus] = useState("all");
  const [filterAssignee, setFilterAssignee] = useState("");
  const [search, setSearch] = useState("");
  const [createForm, setCreateForm] = useState({
    title: "",
    description: "",
    subtasksBullets: "",
    assignee: "",
    priority: "moderate",
    dueDate: "",
  });
  const [progressDraft, setProgressDraft] = useState({ pct: 0, note: "" });
  const [memberUpdate, setMemberUpdate] = useState({
    taskId: "",
    status: "ongoing",
    pct: "0",
    note: "",
    uploadName: "",
  });

  const selectedTask = useMemo(
    () => tasks.find((t) => t.id === selectedId) || emptyTask,
    [tasks, selectedId]
  );

  const isTeamLead = useMemo(() => {
    const isOwner = Boolean(user?.id && activeTeam?.ownerId && activeTeam.ownerId === user.id);
    const role = (user?.position || "").trim().toLowerCase();
    const isLeadRole = role === "project lead" || role === "lead";
    return isOwner && isLeadRole;
  }, [user, activeTeam]);

  const ydocRef = useRef(null);
  const ytextRef = useRef(null);
  const wsRef = useRef(null);
  const applyingRemoteRef = useRef(false);

  const deadlineRadar = useMemo(() => {
    const nowTs = Date.now();
    return tasks
      .filter((task) => task.status !== "done" && task.dueDate)
      .map((task) => {
        const dueTs = new Date(task.dueDate).getTime();
        const diffHours = Math.round((dueTs - nowTs) / 36e5);
        return { ...task, diffHours };
      })
      .sort((a, b) => new Date(a.dueDate) - new Date(b.dueDate))
      .slice(0, 5);
  }, [tasks]);

  const memberProgressStats = useMemo(() => {
    const members = teamMembersMap[activeTeam?.id] || [];
    return members.map((member) => {
      const assigned = tasks.filter(
        (t) =>
          (t.assignee || "").trim().toLowerCase() === member.name.trim().toLowerCase()
      );
      const avg =
        assigned.length > 0
          ? assigned.reduce((sum, t) => sum + Number(t.progressPct || 0), 0) / assigned.length
          : 0;
      return {
        name: member.name,
        position: member.position,
        avgProgress: avg,
      };
    });
  }, [teamMembersMap, activeTeam, tasks]);

  const totalTeamProgress = useMemo(() => {
    if (tasks.length === 0) return 0;
    return tasks.reduce((sum, t) => sum + Number(t.progressPct || 0), 0) / tasks.length;
  }, [tasks]);

  const memberTasks = useMemo(() => {
    if (!user) return [];
    const myName = (user.name || "").trim().toLowerCase();
    const myEmail = (user.email || "").trim().toLowerCase();
    return tasks.filter((task) => {
      const assignee = (task.assignee || "").trim().toLowerCase();
      return assignee && (assignee === myName || assignee === myEmail);
    });
  }, [tasks, user]);

  const visibleTasks = useMemo(() => {
    return isTeamLead ? tasks : memberTasks;
  }, [isTeamLead, tasks, memberTasks]);

  const filteredTasks = useMemo(() => {
    const assigneeNeedle = filterAssignee.trim().toLowerCase();
    const textNeedle = search.trim().toLowerCase();

    return visibleTasks.filter((task) => {
      const statusPass = filterStatus === "all" || task.status === filterStatus;
      const assigneePass =
        assigneeNeedle === "" || (task.assignee || "").toLowerCase().includes(assigneeNeedle);
      const textPass =
        textNeedle === "" ||
        `${task.title} ${task.description}`.toLowerCase().includes(textNeedle);
      return statusPass && assigneePass && textPass;
    });
  }, [visibleTasks, filterStatus, filterAssignee, search]);

  const memberOpenTasks = useMemo(
    () => memberTasks.filter((task) => task.status !== "done"),
    [memberTasks]
  );

  const memberSelectableTasks = useMemo(() => {
    return memberOpenTasks.length > 0 ? memberOpenTasks : memberTasks;
  }, [memberOpenTasks, memberTasks]);

  const memberTaskStats = useMemo(() => {
    const stats = { newTask: 0, ongoingTask: 0, taskClosed: 0 };
    for (const task of memberTasks) {
      if (task.status === "done") {
        stats.taskClosed += 1;
      } else if (task.status === "in-progress" || task.status === "blocked") {
        stats.ongoingTask += 1;
      } else {
        stats.newTask += 1;
      }
    }
    return stats;
  }, [memberTasks]);

  useEffect(() => {
    let cancelled = false;

    async function restoreSession() {
      const raw = localStorage.getItem("smart-planner-session-v1");
      if (!raw) return;
      try {
        const session = JSON.parse(raw);
        if (!session?.user || !session?.activeTeam) {
          return;
        }

        const res = await fetch(
          `${API_BASE}/api/teams/enrolled?userId=${encodeURIComponent(session.user.id)}`
        );
        const enrolled = await res.json();
        const enrolledTeams = Array.isArray(enrolled) ? enrolled : [];
        const matched = enrolledTeams.find((team) => team.id === session.activeTeam.id);

        if (cancelled) return;
        setUser(session.user);
        setTeams(enrolledTeams);

        if (matched) {
          setActiveTeam(matched);
          setPhase("planner");
          return;
        }

        clearSession();
        setActiveTeam(null);
        setPhase("teams");
        setMessage("Session expired. Please select your team again.");
      } catch {
        if (cancelled) return;
        clearSession();
        setUser(null);
        setActiveTeam(null);
        setPhase("auth");
      }
    }

    restoreSession();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (phase !== "planner" || !activeTeam?.id) {
      return;
    }
    loadTasks(activeTeam.id);
    loadMetrics(activeTeam.id);
    loadTeamMembers(activeTeam.id).then((members) => {
      setTeamMembersMap((prev) => ({ ...prev, [activeTeam.id]: members }));
    });
  }, [phase, activeTeam]);

  useEffect(() => {
    if (phase !== "teams" || !user?.id) {
      return;
    }
    refreshTeams(user.id);
    loadInvitations(user.id);
    loadOwnerCodes(user.id);
  }, [phase, user]);

  useEffect(() => {
    if (phase !== "teams" || teams.length === 0) {
      return;
    }
    loadTeamMembersForTeams(teams);
  }, [phase, teams]);

  useEffect(() => {
    if (!selectedTask?.id) return;
    setProgressDraft({
      pct: Number(selectedTask.progressPct || 0),
      note: selectedTask.progressNote || "",
    });
  }, [selectedTask?.id, selectedTask?.progressPct, selectedTask?.progressNote]);

  useEffect(() => {
    setMessage("");
  }, [phase]);

  useEffect(() => {
    if (isTeamLead) return;
    setMemberUpdate((prev) => {
      const hasSelected = prev.taskId && memberTasks.some((task) => task.id === prev.taskId);
      if (hasSelected) {
        return prev;
      }
      if (memberSelectableTasks.length === 0) {
        return { ...prev, taskId: "" };
      }
      return { ...prev, taskId: memberSelectableTasks[0].id };
    });
  }, [isTeamLead, memberTasks, memberSelectableTasks]);

  useEffect(() => {
    if (filteredTasks.length === 0) {
      setSelectedId("");
      return;
    }
    if (!filteredTasks.some((task) => task.id === selectedId)) {
      setSelectedId(filteredTasks[0].id);
    }
  }, [filteredTasks, selectedId]);

  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(timer);
  }, []);

  useEffect(() => {
    if (phase !== "planner") {
      cleanupRealtime();
      return;
    }
    if (!selectedId) {
      cleanupRealtime();
      return;
    }

    cleanupRealtime();

    const doc = new Y.Doc();
    const ytext = doc.getText("description");
    ydocRef.current = doc;
    ytextRef.current = ytext;

    const initial = selectedTask.description || "";
    ytext.insert(0, initial);

    ytext.observe(() => {
      if (!applyingRemoteRef.current) {
        const next = ytext.toString();
        patchTask(selectedId, { description: next }, { updateState: false, refreshMetrics: false });
      }
    });

    doc.on("update", (update, origin) => {
      if (origin === "remote") {
        return;
      }
      if (wsRef.current?.readyState === WebSocket.OPEN) {
        wsRef.current.send(update);
      }
    });

    connectSocket(`${activeTeam.id}:${selectedId}`, doc, ytext);

    return () => {
      cleanupRealtime();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, phase, activeTeam]);

  function saveSession(nextUser, nextTeam) {
    localStorage.setItem(
      "smart-planner-session-v1",
      JSON.stringify({ user: nextUser, activeTeam: nextTeam })
    );
  }

  function clearSession() {
    localStorage.removeItem("smart-planner-session-v1");
  }

  async function loadTasks(teamId) {
    const res = await fetch(`${API_BASE}/api/tasks?teamId=${encodeURIComponent(teamId)}`);
    const data = await res.json();
    setTasks(data);
    if (!selectedId && data.length > 0) {
      setSelectedId(data[0].id);
    }
  }

  async function loadMetrics(teamId) {
    const res = await fetch(`${API_BASE}/api/metrics?teamId=${encodeURIComponent(teamId)}`);
    const data = await res.json();
    setMetrics({ ...emptyMetrics, ...data });
  }

  async function loadInvitations(userId) {
    const res = await fetch(`${API_BASE}/api/teams/invitations?userId=${encodeURIComponent(userId)}`);
    const data = await res.json();
    setInvitations(Array.isArray(data) ? data : []);
  }

  async function loadTeamMembers(teamId) {
    const res = await fetch(`${API_BASE}/api/teams/${teamId}/members`);
    const data = await res.json();
    return Array.isArray(data) ? data : [];
  }

  async function loadTeamMembersForTeams(teamList) {
    const entries = await Promise.all(
      teamList.map(async (team) => [team.id, await loadTeamMembers(team.id)])
    );
    setTeamMembersMap(Object.fromEntries(entries));
  }

  async function loadOwnerCodes(userId) {
    const res = await fetch(`${API_BASE}/api/admin/team-codes?userId=${encodeURIComponent(userId)}`);
    const data = await res.json();
    setOwnerCodes(data && typeof data === "object" ? data : {});
  }

  async function submitAuth(e) {
    e.preventDefault();
    setMessage("");
    const endpoint = authMode === "login" ? "/api/auth/login" : "/api/auth/signup";
    const payload =
      authMode === "login"
        ? { email: profile.email, password: profile.password, role: loginRole }
        : {
            name: profile.name,
            email: profile.email,
            password: profile.password,
            position: profile.position,
          };
    const res = await fetch(`${API_BASE}${endpoint}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    const data = await res.json();
    if (!res.ok) {
      setMessage(data.error || "Authentication failed");
      return;
    }
    const resolvedUser = { ...data.user };
    if (authMode === "login" && loginRole) {
      resolvedUser.position = loginRole;
    }
    setUser(resolvedUser);
    setTeams(data.teams || []);
    setInvitations(data.invitations || []);
    setPhase("teams");
  }

  async function refreshTeams(userId) {
    const res = await fetch(`${API_BASE}/api/teams/enrolled?userId=${encodeURIComponent(userId)}`);
    const data = await res.json();
    setTeams(Array.isArray(data) ? data : []);
  }

  async function createTeamWithMembers(e) {
    e.preventDefault();
    if (!user?.id || !createTeamForm.name.trim()) return;
    const res = await fetch(`${API_BASE}/api/teams/create`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userId: user.id, ...createTeamForm }),
    });
    const data = await res.json();
    if (!res.ok) {
      setMessage(data.error || "Unable to create team");
      return;
    }

    const invitePromises = inviteRows
      .filter((row) => row.email.trim())
      .map((row) =>
        fetch(`${API_BASE}/api/teams/invite`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            teamId: data.id,
            inviterId: user.id,
            email: row.email,
            position: row.position,
          }),
        })
      );
    await Promise.all(invitePromises);

    setCreatedTeam(data);
    setMessage("Team created successfully.");
    setCreateTeamForm({ name: "", description: "", status: "ongoing" });
    setInviteRows([{ email: "", position: "" }]);
    await refreshTeams(user.id);
    await loadOwnerCodes(user.id);
    setPhase("team-created");
  }

  async function copyTeamCode(teamId) {
    const code = ownerCodes[teamId];
    if (!code) return;
    try {
      await navigator.clipboard.writeText(code);
      setMessage("Team code copied to clipboard");
    } catch {
      setMessage(`Team code: ${code}`);
    }
  }

  function openCreateTeamPage() {
    setCreateTeamForm({ name: "", description: "", status: "ongoing" });
    setInviteRows([{ email: "", position: "" }]);
    setPhase("create-team");
  }

  function addInviteRow() {
    setInviteRows((rows) => [...rows, { email: "", position: "" }]);
  }

  function updateInviteRow(index, patch) {
    setInviteRows((rows) => rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  }

  function removeInviteRow(index) {
    setInviteRows((rows) => {
      if (rows.length <= 1) {
        return rows;
      }
      return rows.filter((_, i) => i !== index);
    });
  }

  async function acceptInvite(inviteId) {
    if (!user?.id) return;
    const res = await fetch(`${API_BASE}/api/teams/invitations/${inviteId}/accept`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userId: user.id }),
    });
    const data = await res.json();
    if (!res.ok) {
      setMessage(data.error || "Unable to accept invitation");
      return;
    }
    setMessage(`Joined team ${data.name}`);
    await refreshTeams(user.id);
    await loadInvitations(user.id);
  }

  function pickTeam(team) {
    setPendingTeam(team);
    setTeamCode("");
    setPhase("verify");
    setMessage("");
  }

  async function verifyTeamAccess(e) {
    e.preventDefault();
    if (!user?.id || !pendingTeam?.id) return;
    const res = await fetch(`${API_BASE}/api/teams/verify-code`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ userId: user.id, teamId: pendingTeam.id, code: teamCode }),
    });
    const data = await res.json();
    if (!res.ok) {
      setMessage(data.error || "Invalid code");
      return;
    }
    setActiveTeam(data);
    setPhase("planner");
    saveSession(user, data);
    setMessage("");
  }

  async function createTask(e) {
    e.preventDefault();
    if (!activeTeam?.id) return;
    if (!createForm.title.trim()) return;
    if (!isTeamLead) {
      setMessage("Only project lead can assign tasks.");
      return;
    }

    const dueDateISO = createForm.dueDate
      ? new Date(`${createForm.dueDate}T17:00:00`).toISOString()
      : "";

    const importanceMap = {
      urgent: "high",
      moderate: "medium",
      low: "low",
    };

    const subtasks = createForm.subtasksBullets
      .split("\n")
      .map((line) => line.replace(/^[-*\s]+/, "").trim())
      .filter(Boolean);

    const res = await fetch(
      `${API_BASE}/api/tasks?teamId=${encodeURIComponent(activeTeam.id)}&userId=${encodeURIComponent(user.id)}`,
      {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        title: createForm.title,
        description: createForm.description,
        subtasks,
        assignee: createForm.assignee,
        priority: importanceMap[createForm.priority] || "medium",
        estimateHours: 0,
        dueDate: dueDateISO,
      }),
      }
    );
    const task = await res.json();
    if (!res.ok) {
      setMessage(task.error || "Unable to create task");
      return;
    }
    setTasks((prev) => [task, ...prev]);
    setSelectedId(task.id);
    setCreateForm({
      title: "",
      description: "",
      subtasksBullets: "",
      assignee: "",
      priority: "moderate",
      dueDate: "",
    });
    loadMetrics(activeTeam.id);
  }

  async function submitMemberUpdate(e) {
    e.preventDefault();
    if (!memberUpdate.taskId) {
      setMessage("Please select a task to update.");
      return;
    }

    const statusMap = {
      complete: "done",
      ongoing: "in-progress",
      draft: "todo",
    };

    const pct = Number(memberUpdate.pct || 0);
    if (pct < 0 || pct > 100) {
      setMessage("Percent done must be between 0 and 100.");
      return;
    }

    await patchTask(memberUpdate.taskId, {
      status: statusMap[memberUpdate.status] || "in-progress",
      progressPct: pct,
      progressNote: memberUpdate.note || "",
    });

    if (memberUpdate.status === "complete" || memberUpdate.status === "draft") {
      const label = memberUpdate.uploadName ? `: ${memberUpdate.uploadName}` : "";
      window.alert(`Upload captured${label}`);
    }

    setMemberUpdate((prev) => ({ ...prev, pct: "0", note: "", uploadName: "" }));
  }

  async function patchTask(
    id,
    patch,
    options = { updateState: true, refreshMetrics: true }
  ) {
    const { updateState, refreshMetrics } = options;
    const res = await fetch(
      `${API_BASE}/api/tasks/${id}?teamId=${encodeURIComponent(activeTeam.id)}&userId=${encodeURIComponent(user.id)}`,
      {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
      }
    );
    const nextTask = await res.json();
    if (!res.ok) {
      setMessage(nextTask.error || "Unable to update task");
      return;
    }
    if (updateState) {
      setTasks((prev) => prev.map((t) => (t.id === id ? nextTask : t)));
    }
    if (refreshMetrics) {
      loadMetrics(activeTeam.id);
    }
  }

  async function generateSubtasks() {
    if (!selectedTask.id) return;
    setIsGenerating(true);
    try {
      const res = await fetch(
        `${API_BASE}/api/tasks/${selectedTask.id}/generate-subtasks?teamId=${encodeURIComponent(activeTeam.id)}&userId=${encodeURIComponent(user.id)}`,
        {
        method: "POST",
        }
      );
      const data = await res.json();
      setTasks((prev) =>
        prev.map((t) => (t.id === selectedTask.id ? { ...t, subtasks: data.subtasks || [] } : t))
      );
    } finally {
      setIsGenerating(false);
    }
  }

  async function saveProgressUpdate() {
    if (!selectedTask?.id) return;
    await patchTask(selectedTask.id, {
      progressPct: Number(progressDraft.pct || 0),
      progressNote: progressDraft.note || "",
    });
  }

  function connectSocket(boardId, doc, ytext) {
    const ws = new WebSocket(`${WS_BASE}/ws/crdt?boardId=${boardId}`);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;

    ws.onopen = () => {
      setSyncState("online");
      const state = Y.encodeStateAsUpdate(doc);
      ws.send(state);
    };

    ws.onmessage = (event) => {
      const update =
        typeof event.data === "string"
          ? new TextEncoder().encode(event.data)
          : new Uint8Array(event.data);
      applyingRemoteRef.current = true;
      Y.applyUpdate(doc, update, "remote");
      const liveDescription = ytext.toString();
      const taskID = boardId.includes(":") ? boardId.split(":")[1] : boardId;
      setTasks((prev) => prev.map((t) => (t.id === taskID ? { ...t, description: liveDescription } : t)));
      applyingRemoteRef.current = false;
    };

    ws.onclose = () => {
      setSyncState("reconnecting");
      setTimeout(() => {
        if (`${activeTeam?.id}:${selectedId}` === boardId) {
          connectSocket(boardId, doc, ytext);
        }
      }, 1200);
    };

    ws.onerror = () => {
      setSyncState("offline");
    };
  }

  function cleanupRealtime() {
    if (wsRef.current) {
      wsRef.current.close();
      wsRef.current = null;
    }
    if (ydocRef.current) {
      ydocRef.current.destroy();
      ydocRef.current = null;
      ytextRef.current = null;
    }
  }

  function onDescriptionInput(e) {
    const text = e.target.value;
    const ytext = ytextRef.current;
    if (!ytext) return;

    const previous = ytext.toString();
    if (previous === text) return;

    ytext.delete(0, previous.length);
    ytext.insert(0, text);
    setTasks((prev) => prev.map((t) => (t.id === selectedId ? { ...t, description: text } : t)));
  }

  function dueDateToInput(isoDate) {
    if (!isoDate) return "";
    return new Date(isoDate).toISOString().slice(0, 10);
  }

  function humanDateTime(value) {
    if (!value) return "No deadline";
    return new Date(value).toLocaleString();
  }

  function signOut() {
    const ok = window.confirm("Do you want to log out and go to the main page?");
    if (!ok) {
      return;
    }
    clearSession();
    setPhase("auth");
    setUser(null);
    setActiveTeam(null);
    setTeams([]);
    setInvitations([]);
    setTasks([]);
    setSelectedId("");
    setMessage("");
  }

  function goToMainPage() {
    const ok = window.confirm("Do you want to log out and go to the main page?");
    if (!ok) {
      return;
    }
    clearSession();
    setPhase("auth");
    setUser(null);
    setActiveTeam(null);
    setPendingTeam(null);
    setTeamCode("");
    setTasks([]);
    setSelectedId("");
    setMessage("");
  }

  const ongoingTeams = teams.filter((t) => t.status === "ongoing");
  const completedTeams = teams.filter((t) => t.status === "completed");
  const closedTeams = teams.filter((t) => t.status === "closed");

  if (phase === "auth") {
    return (
      <div className="page">
        <header className="hero">
          <div className="hero-row">
            <p className="chip">Smart Planner</p>
          </div>
          <h1>Team Delivery Workspace</h1>
          <p>
            Plan, track, and deliver large projects with private team spaces, secure team-code
            access, and live execution analytics.
          </p>
        </header>
        <section className="panel auth-panel">
          <div className="auth-toggle">
            <button
              className={authMode === "login" ? "auth-active" : "secondary"}
              onClick={() => setAuthMode("login")}
            >
              Login
            </button>
            <button
              className={authMode === "signup" ? "auth-active" : "secondary"}
              onClick={() => setAuthMode("signup")}
            >
              Create Account
            </button>
          </div>

          <form className="stack-form" onSubmit={submitAuth}>
            {authMode === "signup" ? (
              <input
                value={profile.name}
                onChange={(e) => setProfile((p) => ({ ...p, name: e.target.value }))}
                placeholder="Name"
                required
              />
            ) : null}
            <input
              type="email"
              value={profile.email}
              onChange={(e) => setProfile((p) => ({ ...p, email: e.target.value }))}
              placeholder="Email"
              required
            />
            <input
              type="password"
              value={profile.password}
              onChange={(e) => setProfile((p) => ({ ...p, password: e.target.value }))}
              placeholder="Password"
              required
            />
            {authMode === "login" ? (
              <select value={loginRole} onChange={(e) => setLoginRole(e.target.value)}>
                <option value="member">Member</option>
                <option value="project lead">Project Lead</option>
              </select>
            ) : null}
            {authMode === "signup" ? (
              <input
                value={profile.position}
                onChange={(e) => setProfile((p) => ({ ...p, position: e.target.value }))}
                placeholder="Position"
              />
            ) : null}
            <button type="submit">{authMode === "login" ? "Login" : "Create Account"}</button>
          </form>

          {message ? <p className="error-text">{message}</p> : null}
        </section>
      </div>
    );
  }

  if (phase === "teams") {
    return (
      <div className="page">
        <header className="hero">
          <div className="hero-row">
            <p className="chip">Welcome {user?.name}</p>
            <button className="secondary" onClick={signOut}>
              Log out
            </button>
          </div>
          <h1>Your Teams</h1>
          <p>Select a team, enter the work code, and continue to the private project board.</p>
        </header>

        <section className="panel auth-panel">
          <h3>Ongoing</h3>
          <div className="team-grid">
            {ongoingTeams.map((team) => (
              <div key={team.id} className="team-card">
                <button onClick={() => pickTeam(team)}>
                  <strong>{team.name}</strong>
                  <span>{team.description || "Private workspace"}</span>
                </button>
                {ownerCodes[team.id] ? (
                  <button className="secondary" onClick={() => copyTeamCode(team.id)}>
                    Copy Code
                  </button>
                ) : null}
              </div>
            ))}
          </div>

          <h3>Completed</h3>
          <div className="team-grid">
            {completedTeams.map((team) => (
              <div key={team.id} className="team-card">
                <button onClick={() => pickTeam(team)}>
                  <strong>{team.name}</strong>
                  <span>{team.description || "Completed workspace"}</span>
                </button>
                {ownerCodes[team.id] ? (
                  <button className="secondary" onClick={() => copyTeamCode(team.id)}>
                    Copy Code
                  </button>
                ) : null}
              </div>
            ))}
          </div>

          <h3>Closed</h3>
          <div className="team-grid">
            {closedTeams.map((team) => (
              <div key={team.id} className="team-card">
                <button onClick={() => pickTeam(team)}>
                  <strong>{team.name}</strong>
                  <span>{team.description || "Archived workspace"}</span>
                </button>
                {ownerCodes[team.id] ? (
                  <button className="secondary" onClick={() => copyTeamCode(team.id)}>
                    Copy Code
                  </button>
                ) : null}
              </div>
            ))}
          </div>

          <h3>Invitations</h3>
          <div className="team-grid">
            {invitations.length === 0 ? (
              <p>No pending invitations</p>
            ) : (
              invitations.map((invite) => (
                <div key={invite.id} className="team-card invite-card">
                  <strong>{invite.teamName}</strong>
                  <span>{invite.email}</span>
                  <button onClick={() => acceptInvite(invite.id)}>Accept</button>
                </div>
              ))
            )}
          </div>

          <h3>Create Team</h3>
          <button onClick={openCreateTeamPage}>Create Team</button>
          {message ? <p className="error-text">{message}</p> : null}
        </section>
      </div>
    );
  }

  if (phase === "create-team") {
    return (
      <div className="page">
        <header className="hero">
          <div className="hero-row">
            <p className="chip">Create Team</p>
            <button className="secondary" onClick={signOut}>Log out</button>
          </div>
          <h1>Team Setup</h1>
          <p>Enter team details, add members by email, and create the workspace.</p>
        </header>

        <section className="panel auth-panel">
          <form className="stack-form" onSubmit={createTeamWithMembers}>
            <input
              value={createTeamForm.name}
              onChange={(e) => setCreateTeamForm((f) => ({ ...f, name: e.target.value }))}
              placeholder="Team name"
              required
            />
            <input
              value={createTeamForm.description}
              onChange={(e) => setCreateTeamForm((f) => ({ ...f, description: e.target.value }))}
              placeholder="Team description"
            />
            <select
              value={createTeamForm.status}
              onChange={(e) => setCreateTeamForm((f) => ({ ...f, status: e.target.value }))}
            >
              <option value="ongoing">Ongoing</option>
              <option value="completed">Completed</option>
              <option value="closed">Closed</option>
            </select>

            <h3>Members To Invite</h3>
            {inviteRows.map((row, index) => (
              <div className="member-invite-row" key={index}>
                <input
                  type="email"
                  value={row.email}
                  onChange={(e) => updateInviteRow(index, { email: e.target.value })}
                  placeholder="Member email"
                />
                <input
                  value={row.position}
                  onChange={(e) => updateInviteRow(index, { position: e.target.value })}
                  placeholder="Position"
                />
                <button type="button" className="secondary" onClick={() => removeInviteRow(index)}>
                  Remove
                </button>
              </div>
            ))}

            <button type="button" className="secondary" onClick={addInviteRow}>
              + Add Another Member
            </button>
            <button type="submit">Final Create</button>
            <button type="button" className="secondary" onClick={() => setPhase("teams")}>
              Back To Teams
            </button>
          </form>
          {message ? <p className="error-text">{message}</p> : null}
        </section>
      </div>
    );
  }

  if (phase === "team-created") {
    return (
      <div className="page">
        <header className="hero">
          <p className="chip">Success</p>
          <h1>Team Created</h1>
          <p>{createdTeam?.name} has been created successfully.</p>
        </header>
        <section className="panel auth-panel">
          <p>You can now go back and open this team from the teams page.</p>
          <button onClick={() => setPhase("teams")}>Go Back To Teams</button>
        </section>
      </div>
    );
  }

  if (phase === "verify") {
    return (
      <div className="page">
        <header className="hero">
          <div className="hero-row">
            <p className="chip">Security Check</p>
            <button className="secondary" onClick={signOut}>Log out</button>
          </div>
          <h1>Enter Work Team Code</h1>
          <p>Team: {pendingTeam?.name}</p>
        </header>
        <section className="panel auth-panel">
          <form className="stack-form" onSubmit={verifyTeamAccess}>
            <input
              value={teamCode}
              onChange={(e) => setTeamCode(e.target.value)}
              placeholder="Work team code"
              required
            />
            <button type="submit">Verify And Enter</button>
            <button type="button" className="secondary" onClick={() => setPhase("teams")}>
              Back To Teams
            </button>
            <button type="button" className="secondary" onClick={goToMainPage}>Main Page</button>
          </form>
          {message ? <p className="error-text">{message}</p> : null}
        </section>
      </div>
    );
  }

  return (
    <div className="page">
      <header className="hero">
        <div className="hero-row">
          <p className="chip">Self-Healing Smart Planner</p>
          <div className="top-actions">
            <div className="clock">{now.toLocaleString()}</div>
            <button className="secondary" onClick={() => setPhase("teams")}>Back To Teams</button>
            <button className="secondary" onClick={signOut}>Log out</button>
          </div>
        </div>
        <h1>{activeTeam?.name || "Team Workspace"}</h1>
        <p>{activeTeam?.description || "Collaborative team delivery workspace."}</p>
        <p>
          Signed in as <strong>{user?.name}</strong> ({user?.position || "member"}) in team <strong>{activeTeam?.name}</strong>
        </p>
        <p>
          Team Members: {(teamMembersMap[activeTeam?.id] || [])
            .map((m) => `${m.name} (${m.position || "member"})`)
            .join(", ") || "No members"}
        </p>
        <div className={`status status-${syncState}`}>Sync: {syncState}</div>
      </header>

      <section className="metrics-grid">
        <article className="metric-card">
          <span>Total Tasks</span>
          <strong>{metrics.total}</strong>
        </article>
        <article className="metric-card">
          <span>In Progress</span>
          <strong>{metrics.statusCounts["in-progress"] || 0}</strong>
        </article>
        <article className="metric-card danger">
          <span>Overdue</span>
          <strong>{metrics.overdue}</strong>
        </article>
        <article className="metric-card warm">
          <span>Due In 48h</span>
          <strong>{metrics.dueSoon}</strong>
        </article>
      </section>

      <main className="grid">
        <section className="panel left">
          <h3>{isTeamLead ? "All Assigned Tasks" : "My Assigned Tasks"}</h3>
          <div className="filters">
            <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)}>
              <option value="all">All Statuses</option>
              <option value="todo">New</option>
              <option value="in-progress">Ongoing</option>
              <option value="blocked">Blocked</option>
              <option value="done">Closed</option>
            </select>
            <input
              value={filterAssignee}
              onChange={(e) => setFilterAssignee(e.target.value)}
              placeholder="Filter by assignee"
            />
            <input value={search} onChange={(e) => setSearch(e.target.value)} placeholder="Find task" />
          </div>

          <div className="task-list">
            {filteredTasks.length === 0 ? (
              <p>{isTeamLead ? "No tasks in this team yet." : "No tasks assigned to you yet."}</p>
            ) : (
              filteredTasks.map((task) => (
                <button
                  key={task.id}
                  className={`task-item ${task.id === selectedId ? "active" : ""}`}
                  onClick={() => {
                    setSelectedId(task.id);
                    if (!isTeamLead) {
                      setMemberUpdate((prev) => ({ ...prev, taskId: task.id }));
                    }
                  }}
                >
                  <strong>{task.title}</strong>
                  <span>
                    {task.status} | {task.assignee || "unassigned"}
                  </span>
                  <div className="task-meta-row">
                    <span className={`pill p-${task.priority || "medium"}`}>{task.priority || "medium"}</span>
                    <span>{Number(task.progressPct || 0)}%</span>
                    <span>{task.dueDate ? new Date(task.dueDate).toLocaleDateString() : "No deadline"}</span>
                  </div>
                </button>
              ))
            )}
          </div>
        </section>

        <section className="panel right">
          {isTeamLead ? (
            <>
              <h3><strong>Task Assignment</strong></h3>
              <form onSubmit={createTask} className="task-create">
                <input
                  value={createForm.title}
                  onChange={(e) => setCreateForm((f) => ({ ...f, title: e.target.value }))}
                  placeholder="Task"
                  required
                />
                <textarea
                  value={createForm.description}
                  onChange={(e) => setCreateForm((f) => ({ ...f, description: e.target.value }))}
                  placeholder="Description"
                  rows={3}
                />
                <textarea
                  value={createForm.subtasksBullets}
                  onChange={(e) => setCreateForm((f) => ({ ...f, subtasksBullets: e.target.value }))}
                  placeholder={"Subtasks as bullets (one line each)\n- API contract\n- UI implementation\n- Testing"}
                  rows={4}
                />
                <select
                  value={createForm.assignee}
                  onChange={(e) => setCreateForm((f) => ({ ...f, assignee: e.target.value }))}
                >
                  <option value="">Assign to</option>
                  {(teamMembersMap[activeTeam?.id] || []).map((member) => (
                    <option key={member.id} value={member.name}>
                      {member.name}
                    </option>
                  ))}
                </select>
                <select
                  value={createForm.priority}
                  onChange={(e) => setCreateForm((f) => ({ ...f, priority: e.target.value }))}
                >
                  <option value="urgent">Importance: urgent</option>
                  <option value="moderate">Importance: moderate</option>
                  <option value="low">Importance: low</option>
                </select>
                <input
                  type="date"
                  value={createForm.dueDate}
                  onChange={(e) => setCreateForm((f) => ({ ...f, dueDate: e.target.value }))}
                />
                <button type="submit">Post</button>
              </form>

              {selectedTask.id ? (
              <>
                <div className="task-header">
                  <h2>{selectedTask.title}</h2>
                  <button onClick={generateSubtasks} disabled={isGenerating}>
                    {isGenerating ? "Architecting..." : "Generate Sub-tasks"}
                  </button>
                </div>

                <div className="controls-grid">
                  <select
                    value={selectedTask.status}
                    onChange={(e) => patchTask(selectedTask.id, { status: e.target.value })}
                  >
                    <option value="todo">New</option>
                    <option value="in-progress">Ongoing</option>
                    <option value="done">Closed</option>
                  </select>
                  <input
                    value={selectedTask.assignee || ""}
                    placeholder="Assign to"
                    onBlur={(e) => patchTask(selectedTask.id, { assignee: e.target.value })}
                    onChange={(e) =>
                      setTasks((prev) =>
                        prev.map((t) => (t.id === selectedTask.id ? { ...t, assignee: e.target.value } : t))
                      )
                    }
                  />
                  <select
                    value={selectedTask.priority || "medium"}
                    onChange={(e) => patchTask(selectedTask.id, { priority: e.target.value })}
                  >
                    <option value="high">Importance: urgent</option>
                    <option value="medium">Importance: moderate</option>
                    <option value="low">Importance: low</option>
                  </select>
                  <input
                    type="date"
                    value={dueDateToInput(selectedTask.dueDate)}
                    onChange={(e) => {
                      const dueDate = e.target.value
                        ? new Date(`${e.target.value}T17:00:00`).toISOString()
                        : "";
                      patchTask(selectedTask.id, { dueDate });
                    }}
                  />
                </div>

                <div className="deadline-box">
                  <strong>Deadline:</strong> {humanDateTime(selectedTask.dueDate)}
                </div>

                <label>Description</label>
                <textarea value={selectedTask.description || ""} onChange={onDescriptionInput} rows={6} />

                <label>Subtasks</label>
                <ul className="subtasks">
                  {(selectedTask.subtasks || []).map((subtask) => (
                    <li key={subtask}>{subtask}</li>
                  ))}
                </ul>

                <label>Work status update (optional)</label>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={progressDraft.pct}
                  onChange={(e) => setProgressDraft((p) => ({ ...p, pct: e.target.value }))}
                />
                <textarea
                  rows={3}
                  value={progressDraft.note}
                  onChange={(e) => setProgressDraft((p) => ({ ...p, note: e.target.value }))}
                  placeholder={"Description of progress"}
                />
                <button onClick={saveProgressUpdate}>Post</button>
              </>
            ) : (
              <p>Select any assigned task to view details.</p>
            )}
            </>
          ) : (
            <>
              <h3>Update Ongoing Or New Task</h3>
              <form className="stack-form" onSubmit={submitMemberUpdate}>
                <label>Task</label>
                <select
                  value={memberUpdate.taskId}
                  onChange={(e) => setMemberUpdate((prev) => ({ ...prev, taskId: e.target.value }))}
                  required
                >
                  <option value="">Select task</option>
                  {memberSelectableTasks.map((task) => (
                    <option key={task.id} value={task.id}>
                      {task.title}
                    </option>
                  ))}
                </select>

                <label>Status</label>
                <select
                  value={memberUpdate.status}
                  onChange={(e) => setMemberUpdate((prev) => ({ ...prev, status: e.target.value }))}
                >
                  <option value="ongoing">ongoing</option>
                  <option value="complete">complete</option>
                  <option value="draft">draft</option>
                </select>

                <label>Percent done</label>
                <input
                  type="number"
                  min="0"
                  max="100"
                  value={memberUpdate.pct}
                  onChange={(e) => setMemberUpdate((prev) => ({ ...prev, pct: e.target.value }))}
                  required
                />

                <label>Description of progress</label>
                <textarea
                  rows={4}
                  value={memberUpdate.note}
                  onChange={(e) => setMemberUpdate((prev) => ({ ...prev, note: e.target.value }))}
                  placeholder={"- Completed API integration\n- Finished component tests"}
                />

                {memberUpdate.status === "complete" || memberUpdate.status === "draft" ? (
                  <>
                    <label>Upload</label>
                    <input
                      type="file"
                      onChange={(e) =>
                        setMemberUpdate((prev) => ({
                          ...prev,
                          uploadName: e.target.files?.[0]?.name || "",
                        }))
                      }
                    />
                  </>
                ) : null}

                <button type="submit">Post</button>
              </form>
            </>
          )}
        </section>

        <section className="panel full">
          <h3>Deadline Radar</h3>
          {deadlineRadar.length === 0 ? (
            <p>No upcoming deadlines.</p>
          ) : (
            <div className="radar-list">
              {deadlineRadar.map((task) => (
                <div key={task.id} className="radar-item">
                  <strong>{task.title}</strong>
                  <span>{task.assignee || "unassigned"}</span>
                  <span>{task.diffHours < 0 ? `${Math.abs(task.diffHours)}h overdue` : `${task.diffHours}h left`}</span>
                </div>
              ))}
            </div>
          )}

          <h3>Workload by Assignee</h3>
          <div className="radar-list">
            {Object.entries(metrics.workloadByAssignee || {}).map(([name, hours]) => (
              <div key={name} className="radar-item">
                <strong>{name}</strong>
                <span>{Number(hours).toFixed(1)}h pending</span>
              </div>
            ))}
          </div>

          <h3>Task Completion By Members</h3>
          <p><strong>Total Team Progress:</strong> {totalTeamProgress.toFixed(1)}%</p>
          {!isTeamLead ? (
            <div className="radar-list">
              <div className="radar-item"><strong>New Task</strong><span>{memberTaskStats.newTask}</span></div>
              <div className="radar-item"><strong>Ongoing Task</strong><span>{memberTaskStats.ongoingTask}</span></div>
              <div className="radar-item"><strong>Task Closed</strong><span>{memberTaskStats.taskClosed}</span></div>
            </div>
          ) : null}
          <div className="member-graph">
            {memberProgressStats.map((member) => (
              <div key={member.name} className="member-row">
                <div className="member-label">
                  <strong>{member.name}</strong>
                  <span>{member.position || "member"}</span>
                </div>
                <div className="bar-wrap">
                  <div className="bar-fill" style={{ width: `${member.avgProgress}%` }} />
                </div>
                <span className="member-rate">{member.avgProgress.toFixed(0)}%</span>
              </div>
            ))}
          </div>
        </section>
      </main>
    </div>
  );
}
