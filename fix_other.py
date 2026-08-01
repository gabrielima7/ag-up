import re

with open("main.go", "r") as f:
    content = f.read()

content = content.replace("code := runFlagMode(ctx, &m, *flagAll, *flagCLI, *flagIDE, *flagHub, *flagCheck, maxRetries)", "code := runFlagMode(ctx, m, *flagAll, *flagCLI, *flagIDE, *flagHub, *flagCheck, maxRetries)")
content = content.replace("if err := ui.RunInteractiveMenu(ctx, &m, maxRetries, Version); err != nil {", "if err := ui.RunInteractiveMenu(ctx, m, maxRetries, Version); err != nil {")
content = content.replace("m, err := manifest.Load()", "m, err := manifest.Load()") # unchanged, load returns pointer

with open("main.go", "w") as f:
    f.write(content)

with open("internal/checker/checker.go", "r") as f:
    content = f.read()

content = content.replace("m manifest.Manifest,", "m *manifest.Manifest,")
content = content.replace("localEntry, _ := manifest.Get(m, spec.ID)", "localEntry, _ := manifest.Get(m, spec.ID)")

with open("internal/checker/checker.go", "w") as f:
    f.write(content)

with open("internal/ui/ui.go", "r") as f:
    content = f.read()

content = content.replace("results, err := checker.CheckAll(ctx, allSpecs, *m, maxRetries)", "results, err := checker.CheckAll(ctx, allSpecs, m, maxRetries)")

with open("internal/ui/ui.go", "w") as f:
    f.write(content)

with open("internal/updater/updater.go", "r") as f:
    content = f.read()

content = content.replace("checkResult := checker.Check(ctx, spec, *m, maxRetries)", "checkResult := checker.Check(ctx, spec, m, maxRetries)")
content = content.replace("localEntry, _ := manifest.Get(*m, spec.ID)", "localEntry, _ := manifest.Get(m, spec.ID)")

with open("internal/updater/updater.go", "w") as f:
    f.write(content)

with open("test_simulation.go", "r") as f:
    content = f.read()

content = content.replace("_, err := updater.UpdateAll(ctx, specs, &m, 1)", "_, err := updater.UpdateAll(ctx, specs, m, 1)")

with open("test_simulation.go", "w") as f:
    f.write(content)
