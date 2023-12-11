PKG_ROOT=/tmp
VERSION=$TAG
LUX_ROOT=$PKG_ROOT/node-$VERSION

mkdir -p $LUX_ROOT

OK=`cp ./build/node $LUX_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi
OK=`cp -r ./build/plugins $LUX_ROOT`
if [[ $OK -ne 0 ]]; then
  exit $OK;
fi


echo "Build tgz package..."
cd $PKG_ROOT
echo "Version: $VERSION"
tar -czvf "node-linux-$ARCH-$VERSION.tar.gz" node-$VERSION
aws s3 cp node-linux-$ARCH-$VERSION.tar.gz s3://$BUCKET/linux/binaries/ubuntu/$RELEASE/$ARCH/
