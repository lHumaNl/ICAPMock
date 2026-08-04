# PRD: явное сопоставление ICAP-методов и endpoint через `routes`

## 1. Статус документа

- Статус: implemented; PRD и реализация прошли независимую валидацию.
- Область: v2 scenario YAML (`defaults` и элементы `scenarios`).
- Обратная совместимость: обязательна.
- Владелец реализации: ICAPMock.

## 2. Проблема

Существующие поля `method` и `endpoint` принимают строку или список. Если оба поля содержат
списки, сценарий применим к декартову произведению методов и endpoint:

```yaml
defaults:
  method: [REQMOD, RESPMOD]
  endpoint: [/av/reqmod, /av/respmod]
```

Эта запись создаёт четыре допустимые комбинации:

```text
REQMOD  -> /av/reqmod
REQMOD  -> /av/respmod
RESPMOD -> /av/reqmod
RESPMOD -> /av/respmod
```

Для конфигураций, где каждому ICAP-методу соответствует собственный набор endpoint, это
вынуждает дублировать каждый сценарий. Дублирование увеличивает размер конфигурации, усложняет
поддержку одинаковых условий и ответов и повышает риск расхождения копий.

## 3. Цель

Добавить поле `routes`, которое задаёт точное соответствие ICAP-методов endpoint и может
использоваться как в `defaults`, так и в отдельном сценарии:

```yaml
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD: [/av/respmod, /av/scanfile]
```

Эта запись должна создавать только три пары:

```text
REQMOD  -> /av/reqmod
RESPMOD -> /av/respmod
RESPMOD -> /av/scanfile
```

Все обычные условия и ответы сценария должны применяться к этим маршрутам без дублирования
сценария.

## 4. Не входит в задачу

- Изменение семантики существующих `method` и `endpoint`: их списки по-прежнему образуют
  декартово произведение.
- Добавление `routes` в v1 scenario format.
- Маршрутизация на разные ответы внутри одного `routes`: ответы по-прежнему определяются
  сценарием, `responses` или `branches`.
- Merge сценарного `routes` с `defaults.routes`.
- Изменение глобальной fallback-семантики `defaults.use`.
- Поддержка произвольных ICAP-методов вне `REQMOD`, `RESPMOD`, `OPTIONS`.

## 5. Пользовательский синтаксис

### 5.1. `routes` в `defaults`

```yaml
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD:
      - /av/respmod
      - /av/scanfile
  status: 204
  headers:
    ISTag: '"loadtest-2026"'

scenarios:
  clean:
    when_http:
      headers:
        Content-Type: application/pdf
```

`clean` наследует все три точные пары из `defaults.routes`.

### 5.2. `routes` в сценарии

```yaml
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD: /av/respmod

scenarios:
  special:
    routes:
      RESPMOD: [/av/special, /av/archive]
    status: 204
```

Сценарный `routes` полностью заменяет `defaults.routes`. Для `special` существует только:

```text
RESPMOD -> /av/special
RESPMOD -> /av/archive
```

`REQMOD -> /av/reqmod` не наследуется.

### 5.3. Допустимые формы значения

Значение каждого ключа `routes` принимает:

- один endpoint строкой;
- непустой список endpoint.

```yaml
routes:
  REQMOD: /av/reqmod
  RESPMOD: [/av/respmod, "/tenant/{id}/scan"]
```

Endpoint сохраняют существующую v2-семантику, включая `{name}` captures.

## 6. Правила наследования и override

### 6.1. Общий принцип

Routing specification бывает двух видов:

1. **Legacy routing:** независимые поля `method` и `endpoint`; после наследования их значения
   образуют декартово произведение.
2. **Exact routing:** `routes`; endpoint связаны с конкретными методами.

На одном YAML-уровне (`defaults` либо один сценарий) `routes` нельзя смешивать с `method` или
`endpoint`.

### 6.2. Если `defaults` использует legacy routing

Для существующих конфигураций сохраняется поэлементное наследование:

| Поля routing в сценарии | Effective routing |
|---|---|
| отсутствуют | наследуются `defaults.method` и `defaults.endpoint` |
| только `method` | сценарный `method` + `defaults.endpoint` |
| только `endpoint` | `defaults.method` + сценарный `endpoint` |
| `method` + `endpoint` | оба сценарных значения |
| `routes` | сценарный `routes` полностью заменяет legacy defaults |

После разрешения legacy routing должны присутствовать и методы, и endpoint.

### 6.3. Если `defaults` использует `routes`

| Поля routing в сценарии | Effective routing |
|---|---|
| отсутствуют | все пары из `defaults.routes` |
| `routes` | сценарный `routes` полностью заменяет `defaults.routes` |
| только `method` | выбираются указанные методы из `defaults.routes` вместе с их endpoint |
| только `endpoint` | endpoint заменяются для всех методов из `defaults.routes` |
| `method` + `endpoint` | явное декартово произведение сценарных значений |

Пример выбора метода с сохранением связанного endpoint:

```yaml
defaults:
  routes:
    REQMOD: /av/reqmod
    RESPMOD: [/av/respmod, /av/scanfile]

scenarios:
  request-only:
    method: REQMOD
```

Effective routing:

```text
REQMOD -> /av/reqmod
```

Если сценарий указывает только `method`, каждый указанный метод обязан существовать в
`defaults.routes`. Иначе загрузка завершается ошибкой: endpoint для такого метода наследовать
неоткуда.

Пример явной замены endpoint для выбранного метода:

```yaml
scenarios:
  request-custom:
    method: REQMOD
    endpoint: /av/custom
```

Effective routing:

```text
REQMOD -> /av/custom
```

Пример замены endpoint для всех методов из `defaults.routes`:

```yaml
scenarios:
  shared-custom:
    endpoint: /av/custom
```

Effective routing:

```text
REQMOD  -> /av/custom
RESPMOD -> /av/custom
```

### 6.4. Если routing отсутствует в `defaults`

Сценарий обязан задать либо непустой `routes`, либо разрешаемую legacy-пару `method` и
`endpoint`. Одиночный `method` или одиночный `endpoint` недостаточен и приводит к ошибке.

### 6.5. Частичный legacy routing в `defaults`

Сохраняется текущая совместимость: `defaults` может задать только одну половину legacy routing,
если каждый сценарий предоставляет недостающую половину. После разрешения конкретного сценария
оба поля обязательны.

## 7. Валидация

### 7.1. Ошибочные конфигурации

Scenario file отклоняется в следующих случаях:

1. `routes` указан вместе с YAML-ключом `method` или `endpoint` на том же уровне, даже если
   конфликтующий legacy-ключ имеет `null`, пустую строку или пустой список.
2. `routes` пуст (`routes: {}`) либо имеет `null` вместо map.
3. Ключ route method пуст, записан не в canonical upper-case либо не равен `REQMOD`/`RESPMOD`.
4. Значение route method — пустая строка, пустой список либо `null`.
5. Endpoint не является строкой или списком строк.
6. Endpoint пуст либо состоит только из пробелов.
7. Один endpoint повторяется внутри списка одного метода.
8. Сценарий после наследования/override не имеет ни одной пары method/endpoint.
9. Сценарий содержит только `method` поверх `defaults.routes`, но выбранный метод отсутствует в
   default map.
10. Сценарий содержит только одну legacy-половину, а defaults не позволяет разрешить вторую.
11. Exact route использует endpoint, конфликтующий с reserved service path согласно 8.4.

Один endpoint разрешено указывать под разными методами: это корректные разные exact pairs.
Семантически эквивалентные literal/regex patterns разрешены; выбор сценария определяется обычным
priority/file-order matching.

### 7.2. Presence semantics

- Для конфликта `routes` с `method`/`endpoint` учитывается присутствие YAML-ключа, а не только
  декодированное непустое значение.
- `routes: null`, `routes: {}` и пустые route endpoint являются ошибкой, а не способом очистить
  inherited routes.
- В legacy-only конфигурациях явно пустые/null `method` и `endpoint` сохраняют прежнюю семантику:
  считаются отсутствующими и не ломают существующее наследование.
- Поверх `defaults.routes` пустой/null scenario `method` или `endpoint` также считается
  отсутствующим; если оба effectively отсутствуют, наследуется весь default route map.

### 7.3. Граница отказа

- `ScenarioRegistry.Load` и `validate-scenarios` отклоняют весь некорректный файл; validation CLI
  завершает работу с non-zero exit code.
- Initial server startup сохраняет текущую operational policy: ошибка scenario load логируется,
  а решение о продолжении запуска остаётся за существующим wiring; эта функция не меняет startup
  policy.
- Runtime management reload валидирует replacement до swap. При ошибке старый registry остаётся
  активным.
- Reload, меняющий множество literal/pattern endpoint, отклоняется с сообщением о необходимости
  перезапуска. Изменение method mapping, conditions и responses на том же множестве endpoint
  допускается.

Ошибки содержат имя сценария либо `defaults`, проблемное поле, причину и рекомендацию.

## 8. Семантика выполнения

### 8.1. Matching

Для exact routing request совпадает только тогда, когда одновременно:

1. `req.Method` присутствует в effective route map;
2. URI path совпадает с endpoint, закреплённым именно за этим методом.

Совпадение метода с endpoint другого метода запрещено. Остальные условия (`when`, `when_http`,
body pattern, client IP, priority, branches) применяются без изменений после routing gate.

### 8.2. Captures

`{name}` captures извлекаются только из endpoint текущего ICAP-метода. Endpoint другого метода
не рассматриваются и не могут записать captures.

### 8.3. Service dispatch и global fallback

Router отвечает за попадание запроса в правильный protocol handler, а exact route map — за выбор
сценария. Router не является вторым scenario matcher.

Если path объявлен хотя бы одним сценарием, request dispatch выполняется по фактическому
`req.Method`. Неразрешённая exact cross-pair не может выбрать exact-routed сценарий, но может дойти
до существующего global `defaults.use` fallback. Это намеренно сохраняет глобальную fallback
семантику.

Следовательно, требование «неразрешённая пара не проходит» относится к scenario matching и
sharded candidate eligibility, но не к protocol dispatch на уже известном service path.

### 8.4. Literal, pattern и reserved endpoints

- Literal endpoint регистрируется как exact router path.
- Endpoint с `{name}` и endpoint с `re:` регистрируются как pattern route.
- Перекрывающиеся patterns допустимы. Pattern router используется только как ingress в общий
  method-correct processor; итоговый winner определяется registry priority/file order, поэтому
  registration order не меняет scenario outcome.
- Встроенные `/reqmod`, `/respmod`, `/options` являются reserved service paths.
- Exact routes могут использовать `/reqmod` только для `REQMOD` и `/respmod` только для
  `RESPMOD`. `/options` в `routes` запрещён.
- Pattern route, совпадающий с любым reserved path, отклоняется, чтобы не создавать недостижимые
  или зависящие от router precedence конфигурации.
- Legacy-конфигурации с reserved paths сохраняют текущее поведение.

### 8.5. Sharded registry

Sharded index создаёт ключи только для effective pairs. Один exact-routed runtime scenario может
быть проиндексирован в нескольких shards по каждому literal endpoint без размножения scenario
identity. Повторное появление одного указателя в candidate list должно дедуплицироваться.

Literal exact routes не должны массово деградировать в `globalScenarios`. Capture/raw-regex
routes могут использовать global priority path, если их нельзя безопасно адресовать одним shard.
Standard и sharded registry возвращают одинаковый winner/no-match результат.

### 8.6. Scenario identity и метрики

Один YAML-сценарий остаётся одним runtime-сценарием независимо от количества routes:

- имя, priority и file-order tie-break не меняются;
- route expansion не добавляет runtime scenarios;
- существующая convention с implicit default scenario сохраняется;
- labels `scenario`/`response` не получают технических суффиксов;
- weighted-response sequence применяется как раньше.

### 8.7. Reload

Registry replacement остаётся атомарным. До swap сравнивается нормализованное множество endpoint
старого и нового registry:

- одинаковое множество endpoint: reload разрешён, включая изменение exact method mapping;
- добавление, удаление либо изменение literal/pattern endpoint: reload отклонён, старый registry и
  router table остаются активны, пользователь получает рекомендацию перезапустить процесс.

Это ограничение действует до появления отдельного атомарного router-table replacement API.

## 9. Совместимость

Существующие конфигурации без `routes` не меняют поведение: scalar/list legacy fields,
декартово произведение, partial overrides, v1 format и `defaults.use` работают как раньше.
`routes` является opt-in расширением v2 format. Новые validation rules для reserved paths и
OPTIONS применяются только к effective exact routing.

## 10. Детерминизм

- Canonical method order: `REQMOD`, затем `RESPMOD`.
- Endpoint одного метода сохраняют declaration order.
- Flattened `Paths` строится canonical method traversal с first-occurrence deduplication.
- Один endpoint под разными методами остаётся в exact map обоих методов, но появляется один раз в
  flattened union.
- Duplicate endpoint внутри одного method route является ошибкой.
- Duplicate methods в legacy lists сохраняют существующее поведение.
- Router logs и CLI output сортируют методы canonical order и не зависят от Go map iteration.

## 11. Диагностика и CLI

- `validate-scenarios` реализует validation/failure semantics раздела 7.
- Winner/no-match результат `match-test` должен совпадать с runtime registry для того же
  загруженного набора файлов и request.
- `match-test --verbose` показывает exact method/path pair либо причину несовпадения.
- Directory loading в `match-test` использует ту же merge/order/error semantics, что runtime
  scenario directory loading; route-aware explanation не может использовать отдельный упрощённый
  winner algorithm.
- Startup registration logs показывают нормализованное множество методов на endpoint, но не
  обещают, что каждая method/path cross-pair выберет ordinary scenario: global fallback остаётся
  возможным.

## 12. Рекомендуемое runtime-представление

`MatchRule` получает exact route map рядом с legacy `Methods`/`Paths`, чтобы один YAML-сценарий не
размножался:

- `Routes` — источник истины для совместного method/path matching;
- `Methods`/`Paths` содержат детерминированные union-значения для существующей response validation
  и диагностики;
- union-поля не применяются как независимый gate, когда `Routes` непуст;
- compiled endpoints доступны по method;
- router registration и sharded index читают exact map напрямую.

При legacy routing `Routes` пуст, текущая независимая проверка `Methods` + `Paths` сохраняется.
YAML presence (`routes`, `method`, `endpoint`) хранится отдельно от decoded values для правил 7.2.

## 13. Требования к тестированию

### 13.1. Parsing и validation

- scalar/list routes в defaults и scenario;
- empty/null map и endpoint;
- invalid/case-wrong/OPTIONS method;
- duplicate endpoint и shared endpoint across methods;
- presence-based conflicts, включая `method: null` рядом с `routes`;
- reserved literal и pattern conflicts;
- actionable location-aware errors.

### 13.2. Resolution matrix

Table-driven tests покрывают все строки 6.2/6.3: inherit, full replacement, method-only selection,
endpoint-only replacement, method+endpoint Cartesian replacement, legacy partial override и
missing selected method.

### 13.3. Matching и captures

Для `REQMOD -> /req`, `RESPMOD -> /resp` проверяются четыре комбинации; совпадают только две.
Матрица повторяется для capture и raw-regex endpoint, включая отсутствие чужих captures.
Cross-pair на известном path отдельно проверяет global fallback behavior.

### 13.4. Registry parity и sharding

- Standard/sharded registry дают одинаковый winner/no-match для общей матрицы.
- Literal multi-route scenario реально присутствует в pair-specific shard indexes, а не только в
  global fallback.
- Candidate deduplication исключает повторную проверку одного scenario pointer.
- Capture/regex global indexing не меняет priority outcome.

### 13.5. Router registration

- Literal, capture и raw-regex endpoint достигают method-correct processor.
- Overlapping patterns не меняют registry winner.
- Reserved paths принимают только разрешённые exact mappings.
- Cross-pair не выбирает exact scenario, но может выбрать `defaults.use` fallback.

### 13.6. Reload

- Response/condition/method mapping reload на неизменном endpoint set успешен.
- Add/remove/change endpoint reload отклонён и сохраняет прежний registry/router behavior.
- Invalid routes reload сохраняет предыдущую рабочую конфигурацию.

### 13.7. Regression

- legacy multi-method/multi-endpoint tests проходят без изменения ожиданий;
- route expansion добавляет zero runtime scenarios, а implicit default counting не меняется;
- priority/file order и stream-source method union не меняются;
- `defaults.use` остаётся глобальным;
- initial startup load-error policy не меняется.

## 14. Документация

После реализации обновляются `configs/README.md`, commented examples в
`configs/scenarios/example/example.yaml`, краткое feature description в `README.md` и route-aware
CLI diagnostics. Ограничение reload endpoint topology и запрет OPTIONS должны быть явно указаны.

## 15. Критерии приёмки

1. `routes` работает в defaults и scenario, scenario map полностью заменяет default map.
2. Все partial overrides работают согласно разделу 6.
3. Неразрешённая cross-pair не выбирает exact scenario ни в standard, ни в sharded registry.
4. Global fallback, legacy behavior, scenario identity и counting conventions сохранены.
5. Invalid/ambiguous/unsupported exact routing отклоняется согласно разделу 7.
6. Literal routes сохраняют эффективную pair-specific sharded indexing.
7. Pattern/reserved endpoint behavior соответствует разделу 8.4.
8. Reload либо безопасно применяет изменения на прежнем endpoint set, либо атомарно отклоняет
   topology change с сохранением старого состояния.
9. Runtime registry и CLI дают одинаковый winner/no-match result.
10. Targeted race tests и полный test/lint/vet pass успешны.

## 16. Процессный quality gate

До реализации PRD проходит независимую валидацию. После реализации выполняется отдельный
independent code review; blocker/high findings устраняются до handoff.
