'use strict';

const $ = (id) => document.getElementById(id);

const currencyFormatter = new Intl.NumberFormat('pt-BR', {
  style: 'currency',
  currency: 'BRL',
});

const statusLabels = new Map([
  ['ACTIVE', 'Pendente'],
  ['COMPLETED', 'Paga'],
  ['EXPIRED', 'Expirada'],
  ['CANCELED', 'Cancelada'],
  ['CANCELLED', 'Cancelada'],
]);

let currentCharge;
let pollTimer;
let copyFeedbackTimer;

function isRecord(value) {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function stringValue(value) {
  if (typeof value !== 'string') {
    return '';
  }

  return value.trim();
}

function errorMessage(error) {
  if (error instanceof Error) {
    return error.message;
  }

  return 'Não foi possível concluir a solicitação.';
}

function showFeedback(id, message) {
  const feedback = $(id);
  feedback.textContent = message;
  feedback.hidden = message.length === 0;
}

function extractApiError(data, response) {
  if (isRecord(data)) {
    if (typeof data.error === 'string' && data.error.length > 0) {
      return data.error;
    }

    if (Array.isArray(data.errors)) {
      const messages = [];
      for (const item of data.errors) {
        if (isRecord(item) && typeof item.message === 'string') {
          messages.push(item.message);
        }
      }

      if (messages.length > 0) {
        return messages.join(', ');
      }
    }
  }

  if (typeof data === 'string' && data.trim().length > 0) {
    return data;
  }

  return response.statusText || 'A API retornou um erro.';
}

function recordResponse(response, data) {
  const responseState = $('responseState');
  responseState.textContent = `HTTP ${response.status}`;
  responseState.dataset.state = response.ok ? 'success' : 'error';

  const formattedResponse = typeof data === 'string'
    ? data
    : JSON.stringify(data, undefined, 2);
  $('raw').textContent = formattedResponse || 'Resposta vazia.';
}

async function api(method, path, body) {
  const options = {
    method,
    headers: {},
  };

  if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json';
    options.body = JSON.stringify(body);
  }

  const response = await fetch(path, options);
  const responseText = await response.text();
  let data = responseText;

  try {
    data = JSON.parse(responseText);
  } catch {
    data = responseText;
  }

  if (path !== '/api/config') {
    recordResponse(response, data);
  }

  if (!response.ok) {
    throw new Error(`${response.status}: ${extractApiError(data, response)}`);
  }

  return data;
}

function chargeFromResponse(data) {
  const charge = isRecord(data) ? data.charge : undefined;
  if (!isRecord(charge)) {
    throw new Error('A resposta da API não contém uma cobrança válida.');
  }

  return charge;
}

function formatExpiration(value) {
  const expiration = stringValue(value);
  if (expiration.length === 0) {
    return '—';
  }

  const date = new Date(expiration);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }

  return date.toLocaleString('pt-BR');
}

function safeCheckoutUrl(value) {
  const checkoutUrl = stringValue(value);
  if (checkoutUrl.length === 0) {
    return '';
  }

  try {
    const parsedUrl = new URL(checkoutUrl, window.location.origin);
    if (parsedUrl.protocol !== 'http:' && parsedUrl.protocol !== 'https:') {
      return '';
    }

    return parsedUrl.href;
  } catch {
    return '';
  }
}

function updateQrImage(value) {
  const image = $('qrImg');
  const placeholder = $('qrPlaceholder');
  const imageUrl = stringValue(value);

  if (imageUrl.length === 0) {
    image.removeAttribute('src');
    image.hidden = true;
    placeholder.hidden = false;
    return;
  }

  image.onload = () => {
    image.hidden = false;
    placeholder.hidden = true;
  };
  image.onerror = () => {
    image.hidden = true;
    placeholder.hidden = false;
  };
  image.src = imageUrl;
  image.hidden = false;
  placeholder.hidden = true;
}

function updateCheckoutLink(value) {
  const checkoutLink = $('link');
  const unavailable = $('linkUnavailable');
  const checkoutUrl = safeCheckoutUrl(value);

  if (checkoutUrl.length === 0) {
    checkoutLink.removeAttribute('href');
    checkoutLink.hidden = true;
    unavailable.hidden = false;
    return;
  }

  checkoutLink.href = checkoutUrl;
  checkoutLink.hidden = false;
  unavailable.hidden = true;
}

function statusText(statusCode) {
  return statusLabels.get(statusCode) || statusCode || 'Desconhecido';
}

function startPolling() {
  if (pollTimer !== undefined) {
    return;
  }

  $('poll').textContent = 'Atualização automática a cada 3 segundos enquanto aguarda o pagamento.';
  pollTimer = window.setInterval(() => {
    void refreshCharge();
  }, 3000);
}

function stopPolling() {
  if (pollTimer !== undefined) {
    window.clearInterval(pollTimer);
    pollTimer = undefined;
  }

  $('poll').textContent = '';
}

function renderCharge(charge) {
  currentCharge = charge;
  $('empty').hidden = true;
  $('charge').hidden = false;

  updateQrImage(charge.qrCodeImage);

  const chargeStatus = stringValue(charge.status).toUpperCase() || 'UNKNOWN';
  const status = $('status');
  status.textContent = statusText(chargeStatus);
  status.className = `status-pill status-${chargeStatus.toLowerCase().replace(/[^a-z0-9-]/g, '')}`;
  status.setAttribute('aria-label', `Status da cobrança: ${status.textContent}`);
  $('statusText').textContent = chargeStatus;

  const valueInCents = Number(charge.value);
  $('value').textContent = Number.isFinite(valueInCents)
    ? currencyFormatter.format(valueInCents / 100)
    : '—';
  $('corr').textContent = stringValue(charge.correlationID) || '—';
  $('ident').textContent = stringValue(charge.identifier || charge.transactionID) || '—';
  $('exp').textContent = formatExpiration(charge.expiresDate);
  $('brcode').value = stringValue(charge.brCode);
  updateCheckoutLink(charge.paymentLinkUrl);

  const isActive = chargeStatus === 'ACTIVE';
  $('simulate').disabled = !isActive;
  if (isActive) {
    startPolling();
  } else {
    stopPolling();
  }
}

async function refreshCharge() {
  if (!currentCharge) {
    return;
  }

  const correlationID = stringValue(currentCharge.correlationID);
  if (correlationID.length === 0) {
    showFeedback('chargeErr', 'A cobrança não possui um correlation ID para consultar o status.');
    stopPolling();
    return;
  }

  try {
    const data = await api('GET', `/api/charge/${encodeURIComponent(correlationID)}`);
    renderCharge(chargeFromResponse(data));
    showFeedback('chargeErr', '');
  } catch (error) {
    showFeedback('chargeErr', errorMessage(error));
    stopPolling();
  }
}

function buildCustomer() {
  const customerInput = {
    name: $('cname').value.trim(),
    taxID: $('ctax').value.trim(),
    email: $('cemail').value.trim(),
    phone: $('cphone').value.trim(),
  };
  const customer = {};

  for (const [key, value] of Object.entries(customerInput)) {
    if (value.length > 0) {
      customer[key] = value;
    }
  }

  return customer;
}

function validateCustomer(customer) {
  if (Object.keys(customer).length === 0) {
    return true;
  }

  if (!customer.name) {
    showFeedback('formErr', 'Informe o nome do cliente ou limpe os campos opcionais.');
    $('cname').focus();
    return false;
  }

  if (!customer.taxID && !customer.email && !customer.phone) {
    showFeedback('formErr', 'Informe CPF/CNPJ, e-mail ou telefone do cliente.');
    $('ctax').focus();
    return false;
  }

  return true;
}

$('form').addEventListener('submit', async (event) => {
  event.preventDefault();
  showFeedback('formErr', '');

  const amountText = $('valor').value.trim().replace(',', '.');
  const amountInReais = Number(amountText);
  if (!Number.isFinite(amountInReais) || amountInReais <= 0) {
    showFeedback('formErr', 'Informe um valor maior que R$ 0,00.');
    $('valor').focus();
    return;
  }

  const body = {
    value: Math.round(amountInReais * 100),
  };
  const comment = $('comment').value.trim();
  if (comment.length > 0) {
    body.comment = comment;
  }

  const expirationText = $('expires').value.trim();
  if (expirationText.length > 0) {
    const expirationSeconds = Number(expirationText);
    if (!Number.isInteger(expirationSeconds) || expirationSeconds <= 0) {
      showFeedback('formErr', 'A expiração deve ser um número inteiro de segundos maior que zero.');
      $('expires').focus();
      return;
    }

    body.expiresIn = expirationSeconds;
  }

  const customer = buildCustomer();
  if (!validateCustomer(customer)) {
    return;
  }

  if (Object.keys(customer).length > 0) {
    body.customer = customer;
  }

  const createButton = $('create');
  const createLabel = $('createLabel');
  createButton.disabled = true;
  createLabel.textContent = 'Criando cobrança…';

  try {
    stopPolling();
    const data = await api('POST', '/api/charge', body);
    renderCharge(chargeFromResponse(data));
    showFeedback('chargeErr', '');
  } catch (error) {
    showFeedback('formErr', errorMessage(error));
  } finally {
    createButton.disabled = false;
    createLabel.textContent = 'Criar cobrança';
  }
});

function setCopyFeedback(message) {
  $('copyLabel').textContent = message;
  $('copyHint').textContent = message === 'Copiado!'
    ? 'Código Pix copiado para a área de transferência.'
    : 'Código selecionado — copie manualmente.';

  if (copyFeedbackTimer !== undefined) {
    window.clearTimeout(copyFeedbackTimer);
  }

  copyFeedbackTimer = window.setTimeout(() => {
    $('copyLabel').textContent = 'Copiar';
    $('copyHint').textContent = 'Código pronto para copiar';
    copyFeedbackTimer = undefined;
  }, 1800);
}

$('copy').addEventListener('click', async () => {
  const code = $('brcode').value;
  if (code.length === 0) {
    return;
  }

  try {
    await navigator.clipboard.writeText(code);
    setCopyFeedback('Copiado!');
  } catch {
    $('brcode').focus();
    $('brcode').select();
    setCopyFeedback('Selecionado');
  }
});

$('refresh').addEventListener('click', () => {
  void refreshCharge();
});

$('simulate').addEventListener('click', async () => {
  if (!currentCharge) {
    return;
  }

  const transactionID = stringValue(currentCharge.identifier || currentCharge.transactionID);
  if (transactionID.length === 0) {
    showFeedback('chargeErr', 'A cobrança não possui um identificador para simular o pagamento.');
    return;
  }

  const simulateButton = $('simulate');
  const simulateLabel = $('simulateLabel');
  simulateButton.disabled = true;
  simulateLabel.textContent = 'Simulando pagamento…';
  showFeedback('chargeErr', '');

  try {
    await api('POST', `/api/charge/${encodeURIComponent(transactionID)}/simulate`);
    await refreshCharge();
  } catch (error) {
    showFeedback('chargeErr', `${errorMessage(error)}\nSe necessário, simule pelo painel Woovi e atualize o status.`);
  } finally {
    simulateLabel.textContent = 'Simular pagamento';
    simulateButton.disabled = !currentCharge || stringValue(currentCharge.status).toUpperCase() !== 'ACTIVE';
  }
});

function setWebhookState(state, label) {
  const webhookState = $('webhookState');
  webhookState.dataset.state = state;
  $('webhookStateLabel').textContent = label;
}

function formatEventTime(value) {
  const receivedAt = stringValue(value);
  if (receivedAt.length === 0) {
    return new Date().toLocaleTimeString('pt-BR');
  }

  const date = new Date(receivedAt);
  if (Number.isNaN(date.getTime())) {
    return new Date().toLocaleTimeString('pt-BR');
  }

  return date.toLocaleTimeString('pt-BR');
}

function renderWebhookEvent(webhookEvent) {
  const eventBox = $('events');
  const emptyMessage = eventBox.querySelector('[data-events-empty]');
  if (emptyMessage) {
    emptyMessage.remove();
  }

  const eventItem = document.createElement('article');
  eventItem.className = 'event-item';

  const heading = document.createElement('div');
  heading.className = 'event-item-heading';

  const eventName = document.createElement('strong');
  eventName.textContent = stringValue(webhookEvent.event) || 'Evento sem nome';
  heading.append(eventName);

  const timestamp = document.createElement('time');
  timestamp.textContent = formatEventTime(webhookEvent.receivedAt);
  timestamp.dateTime = stringValue(webhookEvent.receivedAt);
  heading.append(timestamp);

  const payload = document.createElement('pre');
  payload.className = 'event-payload';
  payload.textContent = JSON.stringify(webhookEvent.payload, undefined, 2) || 'Sem payload.';

  eventItem.append(heading, payload);
  eventBox.prepend(eventItem);

  while (eventBox.children.length > 12) {
    const oldestEvent = eventBox.lastElementChild;
    if (!oldestEvent) {
      break;
    }

    oldestEvent.remove();
  }

  const eventPayload = webhookEvent.payload;
  const eventCharge = isRecord(eventPayload) && isRecord(eventPayload.charge)
    ? eventPayload.charge
    : undefined;
  const correlationID = eventCharge ? stringValue(eventCharge.correlationID) : '';
  if (currentCharge && correlationID.length > 0 && correlationID === currentCharge.correlationID) {
    void refreshCharge();
  }
}

function connectWebhooks() {
  const eventSource = new EventSource('/api/events');
  eventSource.onopen = () => {
    setWebhookState('connected', 'Ao vivo');
  };
  eventSource.onerror = () => {
    setWebhookState('reconnecting', 'Reconectando');
  };
  eventSource.onmessage = (message) => {
    try {
      const webhookEvent = JSON.parse(message.data);
      if (isRecord(webhookEvent)) {
        renderWebhookEvent(webhookEvent);
      }
    } catch {
      setWebhookState('reconnecting', 'Evento inválido');
    }
  };
}

async function loadConfiguration() {
  try {
    const configuration = await api('GET', '/api/config');
    if (!isRecord(configuration)) {
      throw new Error('A configuração do ambiente está inválida.');
    }

    const isSandbox = configuration.sandbox === true;
    const baseUrl = stringValue(configuration.baseURL) || 'URL não informada';
    const environment = $('env');
    environment.dataset.env = isSandbox ? 'sandbox' : 'production';
    $('envName').textContent = isSandbox ? 'Sandbox' : 'Produção';
    $('envBase').textContent = baseUrl;
    $('envBase').hidden = baseUrl.length === 0;
    $('envSeparator').hidden = baseUrl.length === 0;
  } catch (error) {
    $('env').dataset.env = 'loading';
    $('envName').textContent = 'Ambiente não identificado';
    $('envBase').textContent = '';
    $('envBase').hidden = true;
    $('envSeparator').hidden = true;
    showFeedback('formErr', errorMessage(error));
  }
}

$('hookUrl').textContent = `${window.location.origin}/webhook`;
connectWebhooks();
void loadConfiguration();
