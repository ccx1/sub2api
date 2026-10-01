(function () {
  const ADMIN_EMAIL = 'imicode@163.com';
  const homeConfig = window.SUB2API_HOME_CONFIG || {};
  const yearNode = document.getElementById('current-year');
  const loginLink = document.querySelector('[data-login-link]');
  const messageForm = document.getElementById('message-form');
  const statusNode = document.getElementById('message-status');

  if (yearNode) {
    yearNode.textContent = String(new Date().getFullYear());
  }

  function buildLoginHref() {
    return new URL(homeConfig.loginPath || '/login', window.location.origin).href;
  }

  if (loginLink) {
    loginLink.href = buildLoginHref();
    loginLink.addEventListener('click', function (event) {
      event.preventDefault();

      try {
        window.top.location.href = loginLink.href;
      } catch (error) {
        window.location.href = loginLink.href;
      }
    });
  }

  function setStatus(message) {
    if (!statusNode) {
      return;
    }
    statusNode.textContent = message;
    window.clearTimeout(setStatus.timer);
    setStatus.timer = window.setTimeout(function () {
      statusNode.textContent = '';
    }, 2200);
  }

  function getValue(id) {
    const node = document.getElementById(id);
    return node ? node.value.trim() : '';
  }

  function buildMailBody(name, contact, content) {
    return [
      '称呼：' + (name || '未填写'),
      '联系方式：' + (contact || '未填写'),
      '',
      '留言内容：',
      content,
      '',
      '来源：iCode API 首页留言',
    ].join('\n');
  }

  if (!messageForm) {
    return;
  }

  messageForm.addEventListener('submit', function (event) {
    event.preventDefault();

    const name = getValue('message-name');
    const contact = getValue('message-contact');
    const content = getValue('message-content');

    if (!content) {
      setStatus('请先填写留言内容');
      return;
    }

    const subject = encodeURIComponent('iCode API 首页留言');
    const body = encodeURIComponent(buildMailBody(name, contact, content));
    window.location.href = 'mailto:' + ADMIN_EMAIL + '?subject=' + subject + '&body=' + body;
    setStatus('已唤起邮件客户端，请在邮件窗口中确认发送');
  });
})();
