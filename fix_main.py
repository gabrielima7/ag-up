import re

with open("main.go", "r") as f:
    content = f.read()

content = content.replace("checker.CheckAll(ctx, specs, *m, maxRetries)", "checker.CheckAll(ctx, specs, m, maxRetries)")

with open("main.go", "w") as f:
    f.write(content)
