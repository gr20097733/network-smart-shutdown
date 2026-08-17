# GitHub 发布步骤（中文）

推荐仓库名：`network-smart-shutdown`

## 一、创建 GitHub 仓库

1. 打开 GitHub 首页并登录。
2. 右上角点击 `+`。
3. 点击 `New repository`。
4. `Repository name` 填：`network-smart-shutdown`。
5. `Description` 可填：`Windows 网卡智能关机助手：支持倒计时、定时和按实时总网速条件自动关机。`
6. 选择 `Public` 或 `Private`。
7. **不要勾选** `Add a README file`、`.gitignore`、`Choose a license`，因为本项目已经准备好了这些仓库文件（许可证除外）。
8. 点击 `Create repository`。

## 二、上传源码

### 最简单：网页上传

1. 进入刚创建的空仓库。
2. 点击 `uploading an existing file`。
3. 解压 `network-smart-shutdown-v1.6.0-github-source.zip`。
4. 将解压后的**仓库目录里面的文件和文件夹**全部拖入上传区域，包括：
   - `.github`
   - `.gitattributes`
   - `.gitignore`
   - `main.go`
   - `go.mod`
   - `README.md`
   - `CHANGELOG.md`
   - `SECURITY.md`
   - `RELEASE_NOTES_V1.6.md`
   - `VERSION`
5. 在页面下方 `Commit changes` 中填写：`Initial release v1.6.0`。
6. 点击绿色 `Commit changes`。

> 注意：浏览器有时不会显示以 `.` 开头的文件。解压后请确认 `.github`、`.gitignore`、`.gitattributes` 都存在并一起上传。

## 三、检查自动构建

上传后：

1. 点击仓库顶部 `Actions`。
2. 找到 `Build Windows`。
3. 正常情况下，首次提交后会自动运行。
4. 运行成功后，点进本次任务，在页面底部 `Artifacts` 可以下载自动编译的 Windows x64 EXE。

## 四、发布 V1.6 Release

推荐使用已经准备好的 V1.6 EXE，第一次发布最直观。

1. 回到仓库首页。
2. 右侧找到 `Releases`，点击。
3. 点击 `Draft a new release`。
4. 点击 `Choose a tag`。
5. 输入：`v1.6.0`。
6. 点击 `Create new tag: v1.6.0 on publish`。
7. `Release title` 填：`网卡智能关机助手 V1.6`。
8. 把 `RELEASE_NOTES_V1.6.md` 的内容复制到说明框，或点击 `Generate release notes` 后再补充 V1.6 更新内容。
9. 上传 Release 附件：
   - `网卡智能关机助手_V1.6.exe`
   - `SHA256SUMS.txt`
10. 点击 `Publish release`。

## 五、以后版本如何发布

仓库已配置 `.github/workflows/release.yml`。

后续代码更新后，只要创建并推送一个新标签，例如：

- `v1.6.0`
- `v1.7.0`
- `v2.0.0`

GitHub Actions 会自动：

1. 使用 Go 1.23 构建 Windows x64 EXE。
2. 生成 SHA256 校验文件。
3. 创建对应的 GitHub Release。
4. 将 EXE 和 SHA256 文件自动挂到 Release。

## 六、许可证

当前项目暂未添加开源许可证。

这意味着即使仓库设为 Public，其他人也默认没有获得复制、修改或重新分发源码的授权。

如果希望项目真正作为开源软件发布，可后续添加 MIT、Apache-2.0 等许可证；如果只是公开源码供查看，可以暂时保持现状。
