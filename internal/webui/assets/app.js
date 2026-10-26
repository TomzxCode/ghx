/* ghx web UI: Vue 3 single-page app over the ghx serve JSON API. */
(() => {
  if (!window.Vue) return;
  const { createApp, reactive, watch, nextTick, onMounted, onBeforeUnmount } = Vue;

  // --- API helper -----------------------------------------------------------

  async function api(path, opts) {
    const res = await fetch(path, opts);
    let body = {};
    try { body = await res.json(); } catch (e) { /* non-JSON error body */ }
    if (!res.ok) throw new Error(body.error || res.status + ' ' + res.statusText);
    return body;
  }

  // --- Icons (GitHub octicon paths, MIT licensed) ---------------------------

  const ICONS = {
    'issue-open': '<path d="M8 9.5a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3Z"></path><path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z"></path>',
    'issue-closed': '<path d="M11.28 6.78a.75.75 0 0 0-1.06-1.06L7.25 8.69 5.78 7.22a.75.75 0 0 0-1.06 1.06l2 2a.75.75 0 0 0 1.06 0l3.5-3.5Z"></path><path d="M8 0a8 8 0 1 1 0 16A8 8 0 0 1 8 0ZM1.5 8a6.5 6.5 0 1 0 13 0 6.5 6.5 0 0 0-13 0Z"></path>',
    'pr-open': '<path d="M1.5 3.25a2.25 2.25 0 1 1 3 2.122v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 1.5 3.25Zm5.677-.177L9.573.677A.25.25 0 0 1 10 .854V2.5h1A2.5 2.5 0 0 1 13.5 5v5.628a2.251 2.251 0 1 1-1.5 0V5a1 1 0 0 0-1-1h-1v1.646a.25.25 0 0 1-.427.177L7.177 3.427a.25.25 0 0 1 0-.354ZM3.75 2.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm0 9.5a.75.75 0 1 0 0 1.5.75.75 0 0 0 0-1.5Zm8.25.75a.75.75 0 1 0 1.5 0 .75.75 0 0 0-1.5 0Z"></path>',
    'pr-closed': '<path d="M3.25 1A2.25 2.25 0 0 1 4 5.372v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 3.25 1Zm9.5 5.5a.75.75 0 0 1 .75.75v3.378a2.251 2.251 0 1 1-1.5 0V7.25a.75.75 0 0 1 .75-.75Zm-2.03-5.273a.75.75 0 0 1 1.06 0l.97.97.97-.97a.748.748 0 0 1 1.265.332.75.75 0 0 1-.205.729l-.97.97.97.97a.751.751 0 0 1-.018 1.042.751.751 0 0 1-1.042.018l-.97-.97-.97.97a.749.749 0 0 1-1.275-.326.749.749 0 0 1 .215-.734l.97-.97-.97-.97a.75.75 0 0 1 0-1.06Z"></path>',
    'pr-merged': '<path d="M5.45 5.154A4.25 4.25 0 0 0 9.25 7.5h1.378a2.251 2.251 0 1 1 0 1.5H9.25A5.734 5.734 0 0 1 5 7.123v3.505a2.25 2.25 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.95-.218ZM4.25 13.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5Zm8.5-4.5a.75.75 0 1 0 0-1.5.75.75 0 0 0 0 1.5ZM5 3.25a.75.75 0 1 0-1.5 0 .75.75 0 0 0 1.5 0Z"></path>',
    'pr-draft': '<path d="M3.25 1A2.25 2.25 0 0 1 4 5.372v5.256a2.251 2.251 0 1 1-1.5 0V5.372A2.25 2.25 0 0 1 3.25 1Zm9.5 14a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3Zm0-4.5a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3Zm0-4.5a1.5 1.5 0 1 1 0-3 1.5 1.5 0 0 1 0 3Z"></path>',
  };

  function stateIcon(item) {
    let key;
    if (item.kind === 'issue') {
      key = item.state === 'closed' ? 'issue-closed' : 'issue-open';
    } else if (item.state === 'merged') {
      key = 'pr-merged';
    } else if (item.isDraft) {
      key = 'pr-draft';
    } else if (item.state === 'closed') {
      key = 'pr-closed';
    } else {
      key = 'pr-open';
    }
    const colors = {
      'issue-open': '#1a7f37', 'issue-closed': '#8250df',
      'pr-open': '#1a7f37', 'pr-closed': '#d1242f',
      'pr-merged': '#8250df', 'pr-draft': '#57606a',
    };
    return '<svg class="octicon" viewBox="0 0 16 16" width="16" height="16" fill="' + colors[key] + '" aria-hidden="true">' + ICONS[key] + '</svg>';
  }

  // --- Formatting helpers ---------------------------------------------------

  function relTime(iso) {
    if (!iso) return '';
    const s = (Date.now() - new Date(iso).getTime()) / 1000;
    if (s < 60) return 'just now';
    const units = [[31536000, 'year'], [2592000, 'month'], [604800, 'week'], [86400, 'day'], [3600, 'hour'], [60, 'minute']];
    for (const [sec, name] of units) {
      if (s >= sec) {
        const n = Math.floor(s / sec);
        return n + ' ' + name + (n > 1 ? 's' : '') + ' ago';
      }
    }
    return 'just now';
  }

  function fmtDate(iso) {
    if (!iso) return '';
    return new Date(iso).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }

  function labelStyle(l) {
    if (!l.color) return { background: '#d0d7de', color: '#24292f' };
    const hex = l.color.replace('#', '');
    const full = hex.length === 3 ? hex.split('').map((c) => c + c).join('') : hex;
    const r = parseInt(full.slice(0, 2), 16);
    const g = parseInt(full.slice(2, 4), 16);
    const b = parseInt(full.slice(4, 6), 16);
    const lum = (0.299 * r + 0.587 * g + 0.114 * b) / 255;
    return { background: '#' + full, color: lum > 0.6 ? '#24292f' : '#ffffff' };
  }

  // --- State ----------------------------------------------------------------

  const state = reactive({
    repos: [],
    route: parseHash(),
    theme: document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light',
    tab: 'issues',
    stateFilter: 'open',
    query: '',
    items: [],
    itemsCount: 0,
    loading: false,
    error: '',
    detail: null,
    refreshing: {},
    repoFilter: '',
    palette: {
      open: false,
      query: '',
      scope: 'repo',
      results: [],
      selected: 0,
      loading: false,
    },
  });

  // --- Routing --------------------------------------------------------------
  // #/                          home (redirects to the first cached repo)
  // #/{owner}/{repo}?tab=&state=   list view
  // #/{owner}/{repo}/issues/{n}    issue detail
  // #/{owner}/{repo}/pulls/{n}     PR detail

  function parseHash() {
    const raw = location.hash.replace(/^#/, '');
    const [path, qs] = raw.split('?');
    const parts = path.split('/').filter(Boolean).map(decodeURIComponent);
    const params = new URLSearchParams(qs || '');
    if (parts.length === 2) return { name: 'list', owner: parts[0], repo: parts[1], params };
    if (parts.length === 4 && (parts[2] === 'issues' || parts[2] === 'pulls')) {
      const number = parseInt(parts[3], 10);
      if (Number.isInteger(number)) {
        return { name: 'detail', owner: parts[0], repo: parts[1], kind: parts[2], number, params };
      }
    }
    return { name: 'home', params: new URLSearchParams() };
  }

  function go(hash) { location.hash = hash; }

  function updateHashQuery(changes) {
    const r = state.route;
    if (r.name !== 'list') return;
    const params = new URLSearchParams(r.params);
    for (const [k, v] of Object.entries(changes)) {
      if (v === null) params.delete(k); else params.set(k, v);
    }
    const qs = params.toString();
    go('#/' + encodeURIComponent(r.owner) + '/' + encodeURIComponent(r.repo) + (qs ? '?' + qs : ''));
  }

  // currentRepo resolves the host for the route's owner/repo from the repos
  // list, falling back to github.com when the repo is unknown.
  function currentRepo() {
    const r = state.route;
    if (r.name === 'home' || !r.owner) return null;
    const found = state.repos.find((x) => x.owner === r.owner && x.repo === r.repo);
    return { host: found ? found.host : 'github.com', owner: r.owner, repo: r.repo };
  }

  function repoKey(repo) { return repo.host + '/' + repo.owner + '/' + repo.repo; }

  function itemUrl(item) {
    const r = state.route;
    const owner = item.owner || r.owner;
    const repo = item.repo || r.repo;
    const kind = item.kind === 'pr' ? 'pulls' : 'issues';
    return '#/' + encodeURIComponent(owner) + '/' + encodeURIComponent(repo) + '/' + kind + '/' + item.number;
  }

  // --- Data loading -----------------------------------------------------------

  async function loadRepos() {
    try {
      const data = await api('/api/repos');
      state.repos = data.repos;
    } catch (e) {
      state.error = 'Failed to load repositories: ' + e.message;
    }
  }

  let queryTimer = null;
  let fetchSeq = 0;

  async function loadItems() {
    const repo = currentRepo();
    if (!repo) return;
    const seq = ++fetchSeq;
    state.loading = true;
    state.error = '';
    try {
      const kind = state.tab === 'prs' ? 'prs' : 'issues';
      const params = new URLSearchParams({ state: state.stateFilter });
      if (state.query) params.set('q', state.query);
      const data = await api('/api/repos/' + repo.host + '/' + encodeURIComponent(repo.owner) + '/' + encodeURIComponent(repo.repo) + '/' + kind + '?' + params);
      if (seq !== fetchSeq) return;
      state.items = data.items;
      state.itemsCount = data.items.length;
    } catch (e) {
      if (seq !== fetchSeq) return;
      state.error = e.message;
      state.items = [];
    }
    state.loading = false;
  }

  async function loadDetail() {
    const repo = currentRepo();
    const r = state.route;
    if (!repo || r.name !== 'detail') return;
    state.loading = true;
    state.error = '';
    state.detail = null;
    try {
      const data = await api('/api/repos/' + repo.host + '/' + encodeURIComponent(repo.owner) + '/' + encodeURIComponent(repo.repo) + '/' + (r.kind === 'pulls' ? 'prs' : 'issues') + '/' + r.number);
      state.detail = data;
    } catch (e) {
      state.error = e.message;
    }
    state.loading = false;
  }

  async function refreshRepo() {
    const repo = currentRepo();
    if (!repo) return;
    const key = repoKey(repo);
    if (state.refreshing[key]) return;
    state.refreshing[key] = true;
    try {
      await api('/api/repos/' + repo.host + '/' + encodeURIComponent(repo.owner) + '/' + encodeURIComponent(repo.repo) + '/refresh', { method: 'POST' });
      state.error = '';
      await loadRepos();
      if (state.route.name === 'list') await loadItems();
      if (state.route.name === 'detail') await loadDetail();
    } catch (e) {
      state.error = 'Refresh failed: ' + e.message;
    }
    delete state.refreshing[key];
  }

  // --- Route effects ----------------------------------------------------------

  let lastListRepoKey = null;

  function applyRoute() {
    const r = state.route;
    if (r.name === 'home') {
      if (state.repos.length > 0) {
        const first = state.repos[0];
        go('#/' + encodeURIComponent(first.owner) + '/' + encodeURIComponent(first.repo));
      }
      return;
    }
    if (r.name === 'list') {
      // Preserve the text filter across in-list navigation (tab/state
      // changes, back/forward); clear it only when switching repositories.
      clearTimeout(queryTimer);
      const repoKey = r.owner + '/' + r.repo;
      if (lastListRepoKey !== repoKey) {
        state.query = '';
        lastListRepoKey = repoKey;
      }
      state.tab = r.params.get('tab') === 'prs' ? 'prs' : 'issues';
      const s = r.params.get('state');
      // "merged" is only meaningful for PRs; coerce it back to a valid
      // issues filter when switching tabs (e.g. via back/forward).
      const allowed = state.tab === 'prs' ? ['open', 'closed', 'merged', 'all'] : ['open', 'closed', 'all'];
      state.stateFilter = allowed.includes(s) ? s : 'open';
      loadItems();
      return;
    }
    if (r.name === 'detail') {
      loadDetail();
    }
  }

  // activeTab reports which tab the header should highlight. On a detail route
  // the item kind decides, so opening a PR from the issues list (or the
  // palette) still marks Pull requests as active.
  function activeTab() {
    const r = state.route;
    if (r.name === 'detail') return r.kind === 'pulls' ? 'prs' : 'issues';
    return state.tab;
  }

  function setTab(tab) {
    const r = state.route;
    // From a detail view a tab click returns to that kind's list, carrying the
    // status filter along so going back to the list keeps the current view.
    if (r.name === 'detail') {
      const base = '#/' + encodeURIComponent(r.owner) + '/' + encodeURIComponent(r.repo);
      const params = new URLSearchParams();
      if (tab === 'prs') params.set('tab', 'prs');
      // "merged" only exists on the PRs tab; drop it so the URL matches the
      // coerced open filter on the issues tab.
      if (state.stateFilter !== 'open' && !(tab === 'issues' && state.stateFilter === 'merged')) {
        params.set('state', state.stateFilter);
      }
      const qs = params.toString();
      go(base + (qs ? '?' + qs : ''));
      return;
    }
    const changes = { tab: tab === 'prs' ? 'prs' : null };
    // "merged" only exists on the PRs tab; drop it so the URL matches the
    // coerced open filter on the issues tab.
    if (tab !== 'prs' && state.stateFilter === 'merged') changes.state = null;
    updateHashQuery(changes);
  }
  function setFilter(state2) { updateHashQuery({ state: state2 === 'open' ? null : state2 }); }

  // --- Palette ----------------------------------------------------------------

  let paletteTimer = null;
  let paletteSeq = 0;

  function openPalette() {
    state.palette.open = true;
    state.palette.query = '';
    state.palette.selected = 0;
    runPaletteSearch();
    nextTick(() => {
      const el = document.getElementById('palette-input');
      if (el) el.focus();
    });
  }

  function closePalette() { state.palette.open = false; }

  async function runPaletteSearch() {
    const p = state.palette;
    const seq = ++paletteSeq;
    p.loading = true;
    try {
      const params = new URLSearchParams({ q: p.query, scope: p.scope });
      if (p.scope === 'repo') {
        const repo = currentRepo();
        if (repo) {
          params.set('host', repo.host);
          params.set('owner', repo.owner);
          params.set('repo', repo.repo);
        }
      }
      const data = await api('/api/search?' + params);
      if (seq !== paletteSeq) return;
      p.results = data.items;
      p.selected = 0;
    } catch (e) {
      if (seq !== paletteSeq) return;
      p.results = [];
    }
    p.loading = false;
  }

  function moveSel(delta) {
    const p = state.palette;
    p.selected = Math.max(0, Math.min(p.selected + delta, p.results.length - 1));
    nextTick(scrollActivePalette);
  }

  function scrollActivePalette() {
    const el = document.querySelector('.palette-results li.active');
    if (el) el.scrollIntoView({ block: 'nearest' });
  }

  function pickPalette() {
    const p = state.palette;
    const item = p.results[p.selected];
    if (!item) return;
    closePalette();
    go(itemUrl(item));
  }

  function toggleScope() {
    state.palette.scope = state.palette.scope === 'repo' ? 'all' : 'repo';
    runPaletteSearch();
  }

  // --- Theme ------------------------------------------------------------------

  function currentTheme() {
    return document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light';
  }

  function toggleTheme() {
    const next = currentTheme() === 'dark' ? 'light' : 'dark';
    document.documentElement.dataset.theme = next;
    try { localStorage.setItem('ghx-theme', next); } catch (e) { /* private mode */ }
    state.theme = next;
  }

  function closeDropdown() {
    state.repoFilter = '';
    if (document.activeElement && document.activeElement.blur) {
      document.activeElement.blur();
    }
  }

  // filteredRepos narrows the repository dropdown to the entries matching every
  // whitespace-separated term in the filter, against host/owner/repo.
  function filteredRepos() {
    const terms = state.repoFilter.toLowerCase().split(/\s+/).filter(Boolean);
    if (!terms.length) return state.repos;
    return state.repos.filter((r) => {
      const haystack = (r.host + '/' + r.owner + '/' + r.repo).toLowerCase();
      return terms.every((term) => haystack.includes(term));
    });
  }

  // focusRepoFilter moves focus into the filter input when the dropdown opens,
  // so it can be typed in without a second click.
  function focusRepoFilter() {
    nextTick(() => {
      const el = document.querySelector('.repo-filter');
      if (el) el.focus();
    });
  }

  function switchRepo(r) {
    closeDropdown();
    go('#/' + encodeURIComponent(r.owner) + '/' + encodeURIComponent(r.repo));
  }

  // selectFirstRepo opens the first repository matching the current filter.
  function selectFirstRepo() {
    const first = filteredRepos()[0];
    if (first) switchRepo(first);
  }

  // --- Root component -----------------------------------------------------------

  const app = createApp({
    setup() {
      function onGlobalKeydown(e) {
        if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'p') {
          e.preventDefault();
          if (state.palette.open) closePalette(); else openPalette();
          return;
        }
        if (e.key === '/' && !state.palette.open) {
          const tag = document.activeElement && document.activeElement.tagName;
          if (tag !== 'INPUT' && tag !== 'TEXTAREA' && tag !== 'SELECT') {
            e.preventDefault();
            openPalette();
          }
        }
      }

      onMounted(() => {
        window.addEventListener('hashchange', onHashChange);
        window.addEventListener('keydown', onGlobalKeydown);
        loadRepos().then(applyRoute);
      });
      onBeforeUnmount(() => {
        window.removeEventListener('hashchange', onHashChange);
        window.removeEventListener('keydown', onGlobalKeydown);
      });

      function onHashChange() {
        state.route = parseHash();
      }

      watch(() => state.route, applyRoute);
      watch(() => state.palette.query, () => {
        clearTimeout(paletteTimer);
        paletteTimer = setTimeout(runPaletteSearch, 150);
      });
      watch(() => state.query, () => {
        clearTimeout(queryTimer);
        queryTimer = setTimeout(loadItems, 200);
      });

      return {
        state,
        repoKey,
        itemUrl,
        stateIcon,
        relTime,
        fmtDate,
        labelStyle,
        setTab,
        setFilter,
        activeTab,
        refreshRepo,
        openPalette,
        closePalette,
        moveSel,
        pickPalette,
        toggleScope,
        toggleTheme,
        closeDropdown,
        filteredRepos,
        focusRepoFilter,
        selectFirstRepo,
        switchRepo,
        isCurrentRepo(r) {
          const repo = currentRepo();
          return !!repo && repo.owner === r.owner && repo.repo === r.repo;
        },
        stateLabel(item) {
          if (item.state === 'merged') return 'Merged';
          if (item.isDraft && item.state === 'open') return 'Draft';
          return item.state.charAt(0).toUpperCase() + item.state.slice(1);
        },
        reviewerLabel(state2) {
          switch (state2) {
            case 'approved': return 'Approved';
            case 'changes_requested': return 'Changes requested';
            case 'commented': return 'Commented';
            case 'dismissed': return 'Dismissed';
            case 'pending': return 'Pending';
            default: return state2;
          }
        },
        reviewerEmoji(state2) {
          switch (state2) {
            case 'approved': return '✔️';
            case 'changes_requested': return '🚫';
            case 'commented': return '💬';
            case 'dismissed': return '❌';
            case 'pending': return '⏳';
            default: return '•';
          }
        },
        detailNoun() {
          const d = state.detail;
          if (!d) return '';
          return d.kind === 'pr' ? 'pull request' : 'issue';
        },
        repoSelectValue() {
          const repo = currentRepo();
          return repo ? repo.owner + '/' + repo.repo : '';
        },
        isRefreshing() {
          const repo = currentRepo();
          return repo ? !!state.refreshing[repoKey(repo)] : false;
        },
        statsUrl() {
          const repo = currentRepo();
          return repo
            ? '/stats?repo=' + encodeURIComponent(repo.host + '/' + repo.owner + '/' + repo.repo)
            : '/stats';
        },
        processUrl() {
          const repo = currentRepo();
          return repo
            ? '/process?repo=' + encodeURIComponent(repo.host + '/' + repo.owner + '/' + repo.repo)
            : '/process';
        },
      };
    },
    methods: {
      go2(hash) { go(hash); },
      repoIssues() {
        const repo = currentRepo();
        if (!repo) return '';
        const found = this.state.repos.find((x) => x.owner === repo.owner && x.repo === repo.repo);
        return found ? found.issueCount : '';
      },
      repoPRs() {
        const repo = currentRepo();
        if (!repo) return '';
        const found = this.state.repos.find((x) => x.owner === repo.owner && x.repo === repo.repo);
        return found ? found.prCount : '';
      },
      badgeClass(item) {
        if (item.state === 'merged') return 'badge-secondary';
        if (item.isDraft && item.state === 'open') return 'badge-neutral';
        if (item.state === 'closed') return item.kind === 'issue' ? 'badge-secondary' : 'badge-error';
        return 'badge-success';
      },
    },
    template: `
<div class="app">
  <header class="bg-neutral text-neutral-content shadow-sm">
    <div class="flex items-center gap-2 flex-wrap px-4 py-2">
      <a class="btn btn-ghost btn-sm text-base font-bold px-2 text-neutral-content" href="#/">ghx</a>
      <div class="dropdown" v-if="state.repos.length" @keydown.escape="closeDropdown">
        <div tabindex="0" role="button" class="repo-select btn btn-sm btn-ghost text-neutral-content max-w-64 gap-2 font-normal" @focus="focusRepoFilter()">
          <span class="truncate">{{ repoSelectValue() }}</span>
          <svg viewBox="0 0 16 16" width="12" height="12" fill="currentColor" aria-hidden="true"><path d="M12.78 5.22a.749.749 0 0 1 0 1.06l-4.25 4.25a.749.749 0 0 1-1.06 0L3.22 6.28a.749.749 0 1 1 1.06-1.06L8 8.939l3.72-3.719a.749.749 0 0 1 1.06 0Z"></path></svg>
        </div>
        <div class="dropdown-content bg-base-100 text-base-content rounded-box border border-base-300 z-50 w-72 p-2 shadow-xl">
          <input
            class="repo-filter input input-sm input-bordered bg-base-100 text-base-content border-base-300 focus:outline-none w-full mb-1"
            v-model="state.repoFilter"
            type="search"
            placeholder="Filter repositories..."
            autocomplete="off"
            spellcheck="false"
            @keydown.enter.prevent="selectFirstRepo()"
          />
          <ul class="menu p-0 w-full max-h-72 overflow-y-auto overflow-x-hidden flex-nowrap">
            <li v-for="r in filteredRepos()" :key="repoKey(r)">
              <a
                :class="{ 'menu-active': isCurrentRepo(r) }"
                class="justify-between gap-2"
                tabindex="0"
                @mousedown.prevent
                @click="switchRepo(r)"
                @keydown.enter.prevent="switchRepo(r)"
              >
                <span class="truncate min-w-0">{{ r.owner }}/{{ r.repo }}</span>
                <span class="text-xs opacity-60 flex-none">{{ r.issueCount }} issues · {{ r.prCount }} PRs</span>
              </a>
            </li>
            <li v-if="!filteredRepos().length" class="px-2 py-1.5 text-sm opacity-60">No repositories match</li>
          </ul>
        </div>
      </div>
      <nav class="join ml-2" v-if="state.route.name === 'list' || state.route.name === 'detail'">
        <button class="btn btn-sm join-item text-neutral-content" :class="activeTab() === 'issues' ? 'btn-neutral' : 'btn-ghost'" @click="setTab('issues')">
          Issues <span class="badge badge-sm badge-neutral">{{ repoIssues() }}</span>
        </button>
        <button class="btn btn-sm join-item text-neutral-content" :class="activeTab() === 'prs' ? 'btn-neutral' : 'btn-ghost'" @click="setTab('prs')">
          Pull requests <span class="badge badge-sm badge-neutral">{{ repoPRs() }}</span>
        </button>
      </nav>
      <div class="filters flex items-center gap-2 ml-auto" v-if="state.route.name === 'list'">
        <select :value="state.stateFilter" @change="setFilter($event.target.value)" class="state-select select select-sm select-bordered bg-base-100 text-base-content border-base-300 focus:outline-none">
          <template v-if="state.tab === 'prs'">
            <option value="open">Open</option>
            <option value="closed">Closed</option>
            <option value="merged">Merged</option>
            <option value="all">All</option>
          </template>
          <template v-else>
            <option value="open">Open</option>
            <option value="closed">Closed</option>
            <option value="all">All</option>
          </template>
        </select>
        <input class="filter-input input input-sm input-bordered bg-base-100 text-base-content border-base-300 focus:outline-none w-48" v-model="state.query" type="search" placeholder="Filter titles...">
      </div>
      <div class="flex-1" v-else></div>
      <a class="btn btn-sm btn-ghost text-neutral-content" :href="statsUrl()" title="Pull request activity report">
        Stats
      </a>
      <a class="btn btn-sm btn-ghost text-neutral-content" :href="processUrl()" title="Issue and PR flow report">
        Process
      </a>
      <button class="btn btn-sm btn-ghost text-neutral-content" @click="toggleTheme()" :title="'Switch to ' + (state.theme === 'dark' ? 'light' : 'dark') + ' theme'">
        <svg v-if="state.theme === 'dark'" viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M7 12a5 5 0 1 1 5 5 5 5 0 0 1-5-5zM11 3A1 1 0 0 1 13 3L13 4A1 1 0 0 1 11 4ZM17.66 4.93A1 1 0 0 1 19.07 6.34L18.36 7.05A1 1 0 0 1 16.95 5.64ZM21 11A1 1 0 0 1 21 13L20 13A1 1 0 0 1 20 11ZM19.07 17.66A1 1 0 0 1 17.66 19.07L16.95 18.36A1 1 0 0 1 18.36 16.95ZM13 21A1 1 0 0 1 11 21L11 20A1 1 0 0 1 13 20ZM6.34 19.07A1 1 0 0 1 4.93 17.66L5.64 16.95A1 1 0 0 1 7.05 18.36ZM3 13A1 1 0 0 1 3 11L4 11A1 1 0 0 1 4 13ZM4.93 6.34A1 1 0 0 1 6.34 4.93L7.05 5.64A1 1 0 0 1 5.64 7.05Z"></path></svg>
        <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden="true"><path d="M21.64 13a1 1 0 0 0-1.05-.14 8.05 8.05 0 0 1-3.37.73 8.15 8.15 0 0 1-8.14-8.1 8.59 8.59 0 0 1 .25-2A1 1 0 0 0 8 2.36a10.14 10.14 0 1 0 14 11.69 1 1 0 0 0-.36-1.05Z"></path></svg>
      </button>
      <button class="btn btn-sm btn-ghost text-neutral-content" @click="openPalette()" title="Search (Ctrl+P)">
        Search <kbd class="kbd kbd-xs">Ctrl P</kbd>
      </button>
      <button class="btn btn-sm btn-ghost text-neutral-content" @click="refreshRepo()" :disabled="isRefreshing()">
        <span v-if="isRefreshing()" class="loading loading-spinner loading-xs"></span>
        {{ isRefreshing() ? 'Refreshing...' : 'Refresh' }}
      </button>
    </div>
  </header>
  <div class="alert alert-error rounded-none py-2 text-sm" v-if="state.error">
    <span>{{ state.error }}</span>
  </div>

  <main class="max-w-6xl mx-auto p-4">
    <div v-if="state.route.name === 'home' && state.repos.length === 0" class="text-center py-20 text-base-content/60">
      <h2 class="text-xl font-semibold mb-2 text-base-content">No cached repositories</h2>
      <p>Run <code>ghx cache</code> inside a git repository to fetch its issues and pull requests, then reload this page.</p>
    </div>

    <div v-else-if="state.route.name === 'list'" class="list-view">
      <div v-if="state.loading" class="text-center py-16 text-base-content/60">
        <span class="loading loading-spinner loading-md text-primary"></span> Loading...
      </div>
      <div class="card bg-base-100 border border-base-300 overflow-hidden" v-else-if="state.items.length">
        <table class="table">
          <tbody>
            <tr v-for="item in state.items" :key="item.kind + item.number" class="hover cursor-pointer" @click="go2(itemUrl(item))">
              <td class="w-10 pl-4" v-html="stateIcon(item)"></td>
              <td>
                <a class="item-title font-semibold hover:text-primary" :href="itemUrl(item)" @click.stop>{{ item.title }}</a>
                <span class="badge badge-sm label-chip" v-for="l in item.labels" :key="l.name" :style="labelStyle(l)">{{ l.name }}</span>
              </td>
              <td class="text-right text-base-content/60 whitespace-nowrap">#{{ item.number }}</td>
              <td class="text-right text-base-content/60 whitespace-nowrap">{{ item.author }}</td>
              <td class="text-right text-base-content/60 whitespace-nowrap w-12">{{ item.commentCount || '' }}</td>
              <td class="text-right text-base-content/60 whitespace-nowrap pr-4" :title="fmtDate(item.updatedAt)">{{ relTime(item.updatedAt) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="text-center py-16 text-base-content/60" v-else>
        <p>No {{ state.tab === 'prs' ? 'pull requests' : 'issues' }} match the current filters.</p>
      </div>
    </div>

    <div v-else-if="state.route.name === 'detail'" class="detail-view">
      <div v-if="state.loading" class="text-center py-16 text-base-content/60">
        <span class="loading loading-spinner loading-md text-primary"></span> Loading...
      </div>
      <template v-else-if="state.detail">
        <div class="detail-header">
          <h1 class="detail-title text-2xl font-normal">
            {{ state.detail.title }}
            <span class="detail-num text-base-content/60 font-light">#{{ state.detail.number }}</span>
          </h1>
          <div class="detail-meta flex items-center gap-3 flex-wrap mt-1 pb-3 border-b border-base-300 text-base-content/60">
            <span class="state-badge badge" :class="badgeClass(state.detail)">{{ stateLabel(state.detail) }}</span>
            <span>
              <b class="text-base-content">{{ state.detail.author }}</b>
              opened this {{ detailNoun() }} on {{ fmtDate(state.detail.createdAt) }}
            </span>
            <span class="cached-at" v-if="state.detail.cachedAt" :title="fmtDate(state.detail.cachedAt)">
              cached {{ relTime(state.detail.cachedAt) }}
            </span>
            <span class="branches" v-if="state.detail.kind === 'pr' && state.detail.headRefName">
              <code>{{ state.detail.headRefName }}</code> → <code>{{ state.detail.baseRefName }}</code>
            </span>
          </div>
        </div>

        <div class="grid grid-cols-1 lg:grid-cols-[minmax(0,1fr)_260px] gap-6 pt-4">
          <div class="detail-main min-w-0">
            <div class="card bg-base-100 border border-base-300">
              <div class="card-body py-4">
                <div class="markdown" v-html="state.detail.bodyHTML"></div>
              </div>
            </div>
            <div class="card bg-base-100 border border-base-300 mt-3 comment" v-for="(c, i) in state.detail.comments" :key="i">
              <div class="comment-head px-4 py-2 text-sm text-base-content/60">
                <b class="text-base-content">{{ c.author }}</b> commented on {{ fmtDate(c.createdAt) }}
              </div>
              <div class="card-body py-4">
                <div class="markdown" v-html="c.bodyHTML"></div>
              </div>
            </div>
            <div class="text-base-content/60 mt-3" v-if="!state.detail.comments.length">
              {{ state.detail.commentCount ? 'Comments not in cache.' : 'No comments yet.' }}
            </div>
          </div>
          <aside class="detail-side text-sm">
            <section v-if="state.detail.kind === 'pr' && state.detail.reviewers && state.detail.reviewers.length" class="mb-4">
              <h3 class="side-title">Reviewers</h3>
              <div v-for="r in state.detail.reviewers" :key="r.login" class="reviewer-row">
                <span class="truncate">{{ r.login }}</span>
                <span class="reviewer-state" :title="reviewerLabel(r.state)" :aria-label="reviewerLabel(r.state)">{{ reviewerEmoji(r.state) }}</span>
              </div>
            </section>
            <section v-if="state.detail.labels.length" class="mb-4">
              <h3 class="side-title">Labels</h3>
              <span class="badge badge-sm label-chip-side" v-for="l in state.detail.labels" :key="l.name" :style="labelStyle(l)">{{ l.name }}</span>
            </section>
            <section v-if="state.detail.assignees.length" class="mb-4">
              <h3 class="side-title">Assignees</h3>
              <div v-for="a in state.detail.assignees" :key="a" class="mb-0.5">{{ a }}</div>
            </section>
            <section v-if="state.detail.milestone" class="mb-4">
              <h3 class="side-title">Milestone</h3>
              <div>{{ state.detail.milestone.title }}</div>
            </section>
            <section class="mb-4">
              <h3 class="side-title">Links</h3>
              <a class="link link-primary" :href="state.detail.url" target="_blank" rel="noopener">View on GitHub</a>
            </section>
          </aside>
        </div>
      </template>
    </div>
  </main>

  <div class="modal modal-open" v-if="state.palette.open" @mousedown.self="closePalette">
    <div class="modal-box max-w-xl p-0 overflow-hidden">
      <input
        id="palette-input"
        class="input input-bordered w-full rounded-none border-0 border-b border-base-300 h-12 text-base focus:outline-none"
        v-model="state.palette.query"
        :placeholder="'Search ' + (state.palette.scope === 'all' ? 'all repositories' : 'this repository') + '...'"
        @keydown.down.prevent="moveSel(1)"
        @keydown.up.prevent="moveSel(-1)"
        @keydown.enter.prevent="pickPalette()"
        @keydown.esc.prevent="closePalette()"
        @keydown.tab.prevent="toggleScope()"
        autocomplete="off"
        spellcheck="false"
      />
      <ul class="palette-results p-2 max-h-[50vh] overflow-auto list-none m-0">
        <li
          v-for="(item, i) in state.palette.results"
          :key="item.host + '/' + item.owner + '/' + item.repo + '/' + item.kind + '/' + item.number"
          class="flex items-center gap-2 px-2 py-1.5 rounded-lg cursor-pointer"
          :class="{ active: i === state.palette.selected }"
          @mouseenter="state.palette.selected = i"
          @click="pickPalette()"
        >
          <span class="inline-flex flex-none" v-html="stateIcon(item)"></span>
          <span class="flex-1 min-w-0 truncate">{{ item.title }}</span>
          <span class="text-xs text-base-content/60 flex-none">#{{ item.number }}</span>
          <span class="text-xs text-base-content/60 flex-none" v-if="state.palette.scope === 'all'">{{ item.owner }}/{{ item.repo }}</span>
        </li>
        <li class="px-2 py-1.5 text-base-content/60 cursor-default" v-if="!state.palette.results.length && !state.palette.loading">
          No matches
        </li>
      </ul>
      <div class="palette-hints flex gap-4 px-3 py-2 border-t border-base-300 bg-base-200 text-xs text-base-content/60">
        <span><kbd class="kbd kbd-xs">↑</kbd><kbd class="kbd kbd-xs">↓</kbd> navigate</span>
        <span><kbd class="kbd kbd-xs">↵</kbd> open</span>
        <span><kbd class="kbd kbd-xs">tab</kbd> scope: {{ state.palette.scope }}</span>
        <span><kbd class="kbd kbd-xs">esc</kbd> close</span>
      </div>
    </div>
  </div>
</div>
`,
  });
  app.mount('#app');
})();
