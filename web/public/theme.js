try {
  var t = localStorage.getItem("sudoku-theme");
  if (t === "dark" || t === "light") document.documentElement.dataset.theme = t;
} catch (e) {}
