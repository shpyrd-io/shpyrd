// A browser translating the page (Chrome's, Google Translate, Edge's) moves
// the page's text into elements of its own. React, updating what it put
// there, then removes a node, or inserts before one, that is no longer where
// it left it, and the whole page stops ("Failed to execute 'removeChild' /
// 'insertBefore' on 'Node'"). Here those two calls tolerate it: a node no
// longer in its parent is not removed again, and a new one goes at the end
// when the one it was to come before has moved (as React's issue 11538
// suggests). A file of its own, before the page's scripts, so the server's
// Content Security Policy needs no inline script (as theme.js).
(function () {
  if (typeof Node !== "function" || !Node.prototype) return;
  var remove = Node.prototype.removeChild;
  Node.prototype.removeChild = function (child) {
    if (child.parentNode !== this) return child;
    return remove.apply(this, arguments);
  };
  var insert = Node.prototype.insertBefore;
  Node.prototype.insertBefore = function (node, before) {
    if (before && before.parentNode !== this) return insert.call(this, node, null);
    return insert.apply(this, arguments);
  };
})();
