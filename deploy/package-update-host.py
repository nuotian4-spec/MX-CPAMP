#!/usr/bin/env python3
"""从本地 deploy 材料生成独立宿主桥 bootstrap 归档。"""

import argparse
import io
import pathlib
import sys
import tarfile


HERE = pathlib.Path(__file__).resolve().parent
FILES = (
    ('mx-update.py', 0o700),
    ('mx-update-host.py', 0o700),
    ('mx-update-host-install.py', 0o700),
    ('mx-update-host.service', 0o644),
    ('mx-update-operation@.service', 0o644),
    ('README.update-host.zh-CN.md', 0o644),
)


def package(output):
    output = pathlib.Path(output).expanduser().absolute()
    materials = [(name, mode, (HERE / name).read_bytes()) for name, mode in FILES]
    created = False
    try:
        with output.open('xb') as stream:
            created = True
            with tarfile.open(fileobj=stream, mode='w:gz') as archive:
                for name, mode, data in materials:
                    entry = tarfile.TarInfo(name)
                    entry.size = len(data)
                    entry.mode = mode
                    entry.uid = entry.gid = 0
                    entry.uname = entry.gname = 'root'
                    archive.addfile(entry, io.BytesIO(data))
    except BaseException:
        if created:
            output.unlink(missing_ok=True)
        raise
    return output


def main(arguments=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', required=True, type=pathlib.Path, help='新归档的完整输出路径，父目录须已存在')
    args = parser.parse_args(arguments)
    try:
        output = package(args.output)
    except FileExistsError:
        print('输出目标已存在，保留原文件：' + str(args.output), file=sys.stderr)
        return 1
    except OSError as error:
        print('本地材料读取或归档写入失败：' + str(error), file=sys.stderr)
        return 1
    print('宿主桥 bootstrap 归档已生成：' + str(output))
    return 0


if __name__ == '__main__':
    sys.exit(main())
